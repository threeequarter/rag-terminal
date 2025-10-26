package rag

import (
	"path/filepath"
	"testing"

	"rag-terminal/internal/vector"
)

func TestFindMentionedFiles(t *testing.T) {
	// Create sample documents
	docs := []vector.Document{
		{
			ID:       "doc1",
			FilePath: filepath.Clean("C:\\Users\\test\\SQLQuery2.sql"),
			FileName: "SQLQuery2.sql",
		},
		{
			ID:       "doc2",
			FilePath: filepath.Clean("C:\\Users\\test\\SQLQuery2-1.sql"),
			FileName: "SQLQuery2-1.sql",
		},
		{
			ID:       "doc3",
			FilePath: filepath.Clean("C:\\Users\\test\\README.md"),
			FileName: "README.md",
		},
	}

	tests := []struct {
		name          string
		userMessage   string
		expectedCount int
		expectedFiles []string
	}{
		{
			name:          "Single file mentioned by name",
			userMessage:   "Tell me about SQLQuery2.sql",
			expectedCount: 1,
			expectedFiles: []string{"SQLQuery2.sql"},
		},
		{
			name:          "Two files mentioned by name",
			userMessage:   "Tell me difference between SQLQuery2-1.sql and SQLQuery2.sql",
			expectedCount: 2,
			expectedFiles: []string{"SQLQuery2-1.sql", "SQLQuery2.sql"},
		},
		{
			name:          "File mentioned by full path",
			userMessage:   "Analyze C:\\Users\\test\\README.md",
			expectedCount: 1,
			expectedFiles: []string{"README.md"},
		},
		{
			name:          "No file mentioned",
			userMessage:   "What is the meaning of life?",
			expectedCount: 0,
			expectedFiles: []string{},
		},
		{
			name:          "All three files mentioned",
			userMessage:   "Compare SQLQuery2.sql, SQLQuery2-1.sql, and README.md",
			expectedCount: 3,
			expectedFiles: []string{"SQLQuery2.sql", "SQLQuery2-1.sql", "README.md"},
		},
		{
			name:          "Case insensitive matching",
			userMessage:   "Tell me about sqlquery2.SQL",
			expectedCount: 1,
			expectedFiles: []string{"SQLQuery2.sql"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findMentionedFiles(tt.userMessage, docs)

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d files, got %d. Files: %v", tt.expectedCount, len(result), result)
			}

			// Verify expected files are in result
			resultMap := make(map[string]bool)
			for _, path := range result {
				fileName := filepath.Base(path)
				resultMap[fileName] = true
			}

			for _, expectedFile := range tt.expectedFiles {
				if !resultMap[expectedFile] {
					t.Errorf("Expected file %s not found in result: %v", expectedFile, result)
				}
			}
		})
	}
}

func TestFilterChunksByFiles(t *testing.T) {
	chunks := []vector.DocumentChunk{
		{
			DocumentID: "doc1",
			FilePath:   filepath.Clean("C:\\Users\\test\\file1.txt"),
			Content:    "Content from file 1",
		},
		{
			DocumentID: "doc2",
			FilePath:   filepath.Clean("C:\\Users\\test\\file2.txt"),
			Content:    "Content from file 2",
		},
		{
			DocumentID: "doc3",
			FilePath:   filepath.Clean("C:\\Users\\test\\file3.txt"),
			Content:    "Content from file 3",
		},
		{
			DocumentID: "doc1",
			FilePath:   filepath.Clean("C:\\Users\\test\\file1.txt"),
			Content:    "More content from file 1",
		},
	}

	tests := []struct {
		name          string
		filePaths     []string
		expectedCount int
		expectedDocs  []string
	}{
		{
			name:          "Filter single file",
			filePaths:     []string{filepath.Clean("C:\\Users\\test\\file1.txt")},
			expectedCount: 2,
			expectedDocs:  []string{"doc1"},
		},
		{
			name: "Filter multiple files",
			filePaths: []string{
				filepath.Clean("C:\\Users\\test\\file1.txt"),
				filepath.Clean("C:\\Users\\test\\file2.txt"),
			},
			expectedCount: 3,
			expectedDocs:  []string{"doc1", "doc2"},
		},
		{
			name:          "Filter non-existent file",
			filePaths:     []string{filepath.Clean("C:\\Users\\test\\nonexistent.txt")},
			expectedCount: 0,
			expectedDocs:  []string{},
		},
		{
			name: "Filter all files",
			filePaths: []string{
				filepath.Clean("C:\\Users\\test\\file1.txt"),
				filepath.Clean("C:\\Users\\test\\file2.txt"),
				filepath.Clean("C:\\Users\\test\\file3.txt"),
			},
			expectedCount: 4,
			expectedDocs:  []string{"doc1", "doc2", "doc3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterChunksByFiles(chunks, tt.filePaths)

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d chunks, got %d", tt.expectedCount, len(result))
			}

			// Verify document IDs
			docMap := make(map[string]bool)
			for _, chunk := range result {
				docMap[chunk.DocumentID] = true
			}

			for _, expectedDoc := range tt.expectedDocs {
				if !docMap[expectedDoc] {
					t.Errorf("Expected document ID %s not found in result", expectedDoc)
				}
			}
		})
	}
}

func TestFindMentionedFiles_NoDuplicates(t *testing.T) {
	docs := []vector.Document{
		{
			ID:       "doc1",
			FilePath: filepath.Clean("C:\\Users\\test\\file.txt"),
			FileName: "file.txt",
		},
	}

	// Mention the same file multiple times
	userMessage := "Compare file.txt with file.txt and also analyze file.txt"
	result := findMentionedFiles(userMessage, docs)

	if len(result) != 1 {
		t.Errorf("Expected 1 unique file, got %d: %v", len(result), result)
	}
}
