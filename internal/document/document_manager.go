package document

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"rag-terminal/internal/logging"
	"rag-terminal/internal/nexa"
	"rag-terminal/internal/vector"
)

// DocumentManager handles document loading and embedding operations
type DocumentManager struct {
	nexaClient  *nexa.Client
	vectorStore vector.VectorStore
}

// NewDocumentManager creates a new document manager
func NewDocumentManager(nexaClient *nexa.Client, vectorStore vector.VectorStore) *DocumentManager {
	return &DocumentManager{
		nexaClient:  nexaClient,
		vectorStore: vectorStore,
	}
}

// LoadDocuments loads documents from a file or directory path
func (dm *DocumentManager) LoadDocuments(ctx context.Context, chat *vector.Chat, path string) (<-chan string, <-chan error, error) {
	logging.Info("LoadDocuments called: path=%s, chatID=%s", path, chat.ID)

	loader := NewLoader()

	// Load documents
	loadResult, err := loader.LoadPath(ctx, path, chat.ID)
	if err != nil {
		logging.Error("Failed to load documents from path %s: %v", path, err)
		return nil, nil, fmt.Errorf("failed to load documents: %w", err)
	}

	logging.Info("Load result: success=%d, total_chunks=%d, errors=%d",
		loadResult.SuccessCount, loadResult.TotalChunks, len(loadResult.Errors))

	if loadResult.SuccessCount == 0 {
		logging.Error("No supported documents found in path: %s", path)
		return nil, nil, fmt.Errorf("no supported documents found in path")
	}

	// Create response channels
	responseChan := make(chan string, 10)
	errorChan := make(chan error, 1)

	// Process documents asynchronously
	go func() {
		defer close(responseChan)
		defer close(errorChan)

		for _, doc := range loadResult.Documents {
			if err := dm.ProcessDocument(ctx, chat, doc, loader, responseChan); err != nil {
				errorChan <- err
				return
			}
		}
	}()

	return responseChan, errorChan, nil
}

// LoadMultipleDocuments loads documents from multiple file or directory paths
func (dm *DocumentManager) LoadMultipleDocuments(ctx context.Context, chat *vector.Chat, paths []PathDetectionResult) (<-chan string, <-chan error, error) {
	logging.Info("LoadMultipleDocuments called: pathCount=%d, chatID=%s", len(paths), chat.ID)

	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("no paths provided")
	}

	// Create response channels
	responseChan := make(chan string, 10)
	errorChan := make(chan error, 1)

	// Process all paths asynchronously
	go func() {
		defer close(responseChan)
		defer close(errorChan)

		loader := NewLoader()
		totalSuccess := 0

		badgerStore, ok := dm.vectorStore.(*vector.BadgerStore)
		if !ok {
			errorChan <- fmt.Errorf("vector store is not BadgerStore type")
			return
		}

		// Count total documents to embed
		totalDocsToEmbed := 0
		for _, pathResult := range paths {
			loadResult, err := loader.LoadPath(ctx, pathResult.Path, chat.ID)
			if err == nil {
				totalDocsToEmbed += loadResult.SuccessCount
			}
		}

		// Send initial progress
		responseChan <- fmt.Sprintf("@@PROGRESS:0/%d@@", totalDocsToEmbed)

		// Reload loader for actual processing
		loader = NewLoader()

		// Load each path
		for i, pathResult := range paths {
			logging.Info("Processing path %d/%d: %s", i+1, len(paths), pathResult.Path)

			loadResult, err := loader.LoadPath(ctx, pathResult.Path, chat.ID)
			if err != nil {
				logging.Error("Failed to load documents from path %s: %v", pathResult.Path, err)
				responseChan <- fmt.Sprintf("⚠ Failed to load %s: %v\n", pathResult.Path, err)
				continue
			}

			if loadResult.SuccessCount == 0 {
				logging.Info("No supported documents found in path: %s", pathResult.Path)
				responseChan <- fmt.Sprintf("⚠ No supported documents in %s\n", pathResult.Path)
				continue
			}

			// Process documents from this path using helper
			for _, doc := range loadResult.Documents {
				if err := dm.ProcessDocument(ctx, chat, doc, loader, responseChan); err != nil {
					errorChan <- err
					return
				}

				totalSuccess++

				// Send progress update
				responseChan <- fmt.Sprintf("@@PROGRESS:%d/%d@@", totalSuccess, totalDocsToEmbed)
			}
		}

		// Update chat file count
		if totalSuccess > 0 {
			chat.FileCount += totalSuccess
			if err := badgerStore.UpdateChat(ctx, chat); err != nil {
				logging.Error("Failed to update chat file count: %v", err)
			}
		}
	}()

	return responseChan, errorChan, nil
}

// ProcessDocument handles the complete pipeline for a single document
func (dm *DocumentManager) ProcessDocument(
	ctx context.Context,
	chat *vector.Chat,
	doc vector.Document,
	loader *Loader,
	responseChan chan<- string,
) error {
	badgerStore, ok := dm.vectorStore.(*vector.BadgerStore)
	if !ok {
		return fmt.Errorf("vector store is not BadgerStore type")
	}

	logging.Debug("Processing document: %s (size=%d, hash=%s)", doc.FileName, doc.FileSize, doc.ContentHash)

	// Check if document with same content hash already exists
	existingDoc, err := badgerStore.FindDocumentByHash(ctx, doc.ContentHash)
	if err == nil && existingDoc != nil {
		logging.Info("Document %s already exists (duplicate of %s), skipping", doc.FileName, existingDoc.FileName)
		if responseChan != nil {
			responseChan <- fmt.Sprintf("Skipped %s (duplicate of %s)\n", doc.FileName, existingDoc.FileName)
		}
		return nil // Not an error, just skipped
	}

	// Store document metadata
	if err := badgerStore.StoreDocument(ctx, &doc); err != nil {
		logging.Error("Failed to store document metadata for %s: %v", doc.FileName, err)
		return fmt.Errorf("failed to store document %s: %w", doc.FileName, err)
	}
	logging.Debug("Stored document metadata for %s", doc.FileName)

	// Get chunks for this document
	chunks, err := loader.GetDocumentChunks(doc.ID, doc.FilePath, chat.ID)
	if err != nil {
		logging.Error("Failed to get chunks for %s: %v", doc.FileName, err)
		return fmt.Errorf("failed to chunk document %s: %w", doc.FileName, err)
	}
	logging.Debug("Created %d chunks for %s", len(chunks), doc.FileName)

	// Prepare chunk contents for batch embedding
	chunkContents := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkContents[i] = chunk.Content
	}

	// Generate embeddings for all chunks in batch
	logging.Debug("Generating embeddings for %d chunks of %s", len(chunks), doc.FileName)
	embeddings, err := dm.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, chunkContents)
	if err != nil {
		logging.Error("Failed to generate embeddings for %s: %v", doc.FileName, err)
		return fmt.Errorf("failed to generate embeddings for %s: %w", doc.FileName, err)
	}
	logging.Debug("Generated %d embeddings for %s (dim=%d)", len(embeddings), doc.FileName, len(embeddings[0]))

	// Store chunks with embeddings
	for i, chunk := range chunks {
		chunk.Embedding = embeddings[i]
		if err := badgerStore.StoreDocumentChunk(ctx, &chunk); err != nil {
			logging.Error("Failed to store chunk %d of %s: %v", i, doc.FileName, err)
			return fmt.Errorf("failed to store chunk %d of %s: %w", i, doc.FileName, err)
		}
	}
	logging.Info("Successfully stored %d chunks for %s", len(chunks), doc.FileName)

	return nil
}

// FindMentionedFiles checks if the user message mentions any of the loaded document filenames
func (dm *DocumentManager) FindMentionedFiles(userMessage string, docs []vector.Document) []string {
	lowerMessage := strings.ToLower(userMessage)
	var mentionedFiles []string
	seenPaths := make(map[string]bool) // Prevent duplicates

	for _, doc := range docs {
		// Check both full path and just filename
		fileName := filepath.Base(doc.FilePath)
		normalizedPath := filepath.Clean(doc.FilePath)

		if strings.Contains(lowerMessage, strings.ToLower(fileName)) {
			if !seenPaths[normalizedPath] {
				mentionedFiles = append(mentionedFiles, normalizedPath)
				seenPaths[normalizedPath] = true
			}
		} else if strings.Contains(lowerMessage, strings.ToLower(doc.FilePath)) {
			if !seenPaths[normalizedPath] {
				mentionedFiles = append(mentionedFiles, normalizedPath)
				seenPaths[normalizedPath] = true
			}
		}
	}

	return mentionedFiles
}

// FilterChunksByFiles returns only chunks from the specified file paths
func (dm *DocumentManager) FilterChunksByFiles(chunks []vector.DocumentChunk, filePaths []string) []vector.DocumentChunk {
	var filtered []vector.DocumentChunk

	// Normalize all search paths for comparison
	normalizedSearchPaths := make(map[string]bool)
	for _, filePath := range filePaths {
		normalizedSearchPaths[strings.ToLower(filepath.Clean(filePath))] = true
	}

	for _, chunk := range chunks {
		// Normalize stored path for comparison (case-insensitive on Windows)
		normalizedChunkPath := strings.ToLower(filepath.Clean(chunk.FilePath))
		if normalizedSearchPaths[normalizedChunkPath] {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

// GetAllChunksFromFiles retrieves all chunks for multiple specified files
func (dm *DocumentManager) GetAllChunksFromFiles(ctx context.Context, filePaths []string) []vector.DocumentChunk {
	store, ok := dm.vectorStore.(*vector.BadgerStore)
	if !ok {
		logging.Error("Vector store is not BadgerStore type")
		return []vector.DocumentChunk{}
	}

	// Get all documents to find matching document IDs
	docs, err := store.GetDocuments(ctx)
	if err != nil {
		logging.Error("Failed to get documents: %v", err)
		return []vector.DocumentChunk{}
	}

	// Build map of normalized paths to document IDs
	normalizedSearchPaths := make(map[string]bool)
	for _, filePath := range filePaths {
		normalizedSearchPaths[strings.ToLower(filepath.Clean(filePath))] = true
	}
	logging.Debug("Searching for documents with normalized paths: %v", filePaths)

	var targetDocIDs []string
	for _, doc := range docs {
		normalizedDocPath := strings.ToLower(filepath.Clean(doc.FilePath))
		if normalizedSearchPaths[normalizedDocPath] {
			targetDocIDs = append(targetDocIDs, doc.ID)
			logging.Debug("Found matching document ID: %s for path %s", doc.ID, doc.FilePath)
		}
	}

	if len(targetDocIDs) == 0 {
		logging.Error("No documents found for paths: %v", filePaths)
		logging.Error("Available documents: %d", len(docs))
		for i, doc := range docs {
			logging.Error("  [%d] %s", i, doc.FilePath)
		}
		return []vector.DocumentChunk{}
	}

	logging.Info("Found %d document IDs for %d requested paths", len(targetDocIDs), len(filePaths))

	// Create a map for fast lookup
	targetDocIDMap := make(map[string]bool)
	for _, id := range targetDocIDs {
		targetDocIDMap[id] = true
	}

	// Search with a dummy embedding to get all chunks, then filter by document IDs
	dummyEmbedding := make([]float32, 768)
	_, allChunks, err := store.SearchSimilarWithChunks(ctx, dummyEmbedding, 200)
	if err != nil {
		logging.Error("Failed to search chunks: %v", err)
		return []vector.DocumentChunk{}
	}

	var filtered []vector.DocumentChunk
	for _, chunk := range allChunks {
		if targetDocIDMap[chunk.DocumentID] {
			filtered = append(filtered, chunk)
		}
	}

	// Sort by document ID first, then by chunk index to maintain order
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].DocumentID != filtered[j].DocumentID {
			return filtered[i].DocumentID < filtered[j].DocumentID
		}
		return filtered[i].ChunkIndex < filtered[j].ChunkIndex
	})

	logging.Debug("Retrieved %d chunks for %d documents", len(filtered), len(targetDocIDs))
	return filtered
}
