package document

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"rag-chat/internal/vector"
)

// Loader orchestrates document loading, parsing, chunking, and storage
type Loader struct {
	parser  *Parser
	chunker *Chunker
}

// NewLoader creates a new document loader
func NewLoader() *Loader {
	return &Loader{
		parser:  NewParser(),
		chunker: NewChunker(),
	}
}

// LoadResult contains the results of loading documents
type LoadResult struct {
	Documents      []vector.Document
	TotalChunks    int
	TotalFiles     int
	SuccessCount   int
	FailureCount   int
	Errors         []error
	ProcessingTime time.Duration
}

// LoadPath loads a file or directory into documents
func (l *Loader) LoadPath(ctx context.Context, path string, chatID string) (*LoadResult, error) {
	startTime := time.Now()

	result := &LoadResult{
		Documents: []vector.Document{},
		Errors:    []error{},
	}

	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("path does not exist: %w", err)
	}

	// Process based on type
	if info.IsDir() {
		// Load directory recursively
		err = l.loadDirectory(ctx, path, chatID, result)
	} else {
		// Load single file
		err = l.loadFile(ctx, path, chatID, result)
	}

	result.ProcessingTime = time.Since(startTime)

	return result, err
}

// loadDirectory recursively loads all supported files in a directory
func (l *Loader) loadDirectory(ctx context.Context, dirPath string, chatID string, result *LoadResult) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("error accessing %s: %w", path, err))
			return nil // Continue walking
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip hidden files
		if len(info.Name()) > 0 && info.Name()[0] == '.' {
			return nil
		}

		// Try to load the file
		err = l.loadFile(ctx, path, chatID, result)
		if err != nil {
			// Log error but continue processing other files
			result.Errors = append(result.Errors, err)
		}

		return nil
	})
}

// loadFile loads a single file
func (l *Loader) loadFile(ctx context.Context, filePath string, chatID string, result *LoadResult) error {
	result.TotalFiles++

	// Parse the file
	parsed := l.parser.ParseFile(filePath)

	// Check if file is supported
	if !parsed.IsSupported {
		result.FailureCount++
		return fmt.Errorf("file not supported: %s", filePath)
	}

	// Check for parsing errors
	if parsed.Error != nil {
		result.FailureCount++
		return fmt.Errorf("failed to parse %s: %w", filePath, parsed.Error)
	}

	// Create document record
	doc := vector.Document{
		ID:         uuid.New().String(),
		ChatID:     chatID,
		FilePath:   parsed.FilePath,
		FileName:   parsed.FileName,
		FileSize:   parsed.Size,
		Encoding:   parsed.Encoding,
		UploadedAt: time.Now(),
		Metadata:   make(map[string]string),
	}

	// Determine MIME type based on extension
	ext := filepath.Ext(filePath)
	doc.MimeType = getMimeType(ext)

	// Add relative path if loading from directory
	doc.Metadata["extension"] = ext

	// Chunk the document
	chunks := l.chunker.ChunkDocument(parsed.Content)
	doc.ChunkCount = len(chunks)

	result.TotalChunks += len(chunks)
	result.Documents = append(result.Documents, doc)
	result.SuccessCount++

	return nil
}

// GetDocumentChunks returns the chunks for a specific document
func (l *Loader) GetDocumentChunks(docID string, filePath string, chatID string) ([]vector.DocumentChunk, error) {
	// Parse the file
	parsed := l.parser.ParseFile(filePath)
	if parsed.Error != nil {
		return nil, parsed.Error
	}

	// Chunk the document
	chunks := l.chunker.ChunkDocument(parsed.Content)

	// Convert to DocumentChunk models
	result := make([]vector.DocumentChunk, len(chunks))
	for i, chunk := range chunks {
		result[i] = vector.DocumentChunk{
			ID:         uuid.New().String(),
			DocumentID: docID,
			ChatID:     chatID,
			ChunkIndex: chunk.Index,
			Content:    chunk.Content,
			StartPos:   chunk.StartPos,
			EndPos:     chunk.EndPos,
			FilePath:   filePath,
			Embedding:  []float32{}, // Will be populated during embedding
		}
	}

	return result, nil
}

// getMimeType returns a MIME type based on file extension
func getMimeType(ext string) string {
	mimeTypes := map[string]string{
		".txt":  "text/plain",
		".md":   "text/markdown",
		".log":  "text/plain",
		".json": "application/json",
		".xml":  "application/xml",
		".yaml": "application/x-yaml",
		".yml":  "application/x-yaml",
		".csv":  "text/csv",
		".html": "text/html",
		".css":  "text/css",
		".js":   "application/javascript",
		".ts":   "application/typescript",
		".go":   "text/x-go",
		".java": "text/x-java",
		".py":   "text/x-python",
		".c":    "text/x-c",
		".cpp":  "text/x-c++",
		".h":    "text/x-c",
		".rs":   "text/x-rust",
		".sql":  "application/sql",
		".sh":   "application/x-sh",
		".bat":  "application/x-bat",
		".ps1":  "application/x-powershell",
	}

	if mime, exists := mimeTypes[ext]; exists {
		return mime
	}

	return "text/plain"
}

// CalculateDirectoryStats returns statistics about files in a directory
func (l *Loader) CalculateDirectoryStats(dirPath string) (totalFiles int, supportedFiles int, totalSize int64, err error) {
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() {
			return nil
		}

		// Skip hidden files
		if len(info.Name()) > 0 && info.Name()[0] == '.' {
			return nil
		}

		totalFiles++
		totalSize += info.Size()

		// Check if supported
		ext := filepath.Ext(path)
		if IsSupported(ext) {
			supportedFiles++
		}

		return nil
	})

	return
}
