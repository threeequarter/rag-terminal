package rag

import (
	"context"
	"fmt"

	"rag-terminal/internal/document"
	"rag-terminal/internal/logging"
	"rag-terminal/internal/nexa"
	"rag-terminal/internal/vector"
)

// DocumentProcessor handles single document processing: chunking, embedding, and storage
type DocumentProcessor struct {
	vectorStore vector.VectorStore
	nexaClient  *nexa.Client
}

// NewDocumentProcessor creates a new document processor
func NewDocumentProcessor(vectorStore vector.VectorStore, nexaClient *nexa.Client) *DocumentProcessor {
	return &DocumentProcessor{
		vectorStore: vectorStore,
		nexaClient:  nexaClient,
	}
}

// ProcessDocument handles pipeline for a single document:
// duplicate detection, metadata storage, chunking, embedding, and storage
func (dp *DocumentProcessor) ProcessDocument(
	ctx context.Context,
	chat *vector.Chat,
	embedModel string,
	doc vector.Document,
	loader *document.Loader,
	responseChan chan<- string,
) error {
	badgerStore, ok := dp.vectorStore.(*vector.BadgerStore)
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
	logging.Debug("Generating embeddings for %d chunks of %s with dimensions=%d", len(chunks), doc.FileName, 384) // Default dimension
	embeddings, err := dp.nexaClient.GenerateEmbeddings(ctx, embedModel, chunkContents, nil)
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
