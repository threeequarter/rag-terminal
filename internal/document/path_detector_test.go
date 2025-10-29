package document

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAllPaths_Windows(t *testing.T) {
	// Create temp files for testing
	tempDir := t.TempDir()
	file1 := filepath.Join(tempDir, "file1.txt")
	file2 := filepath.Join(tempDir, "file2.txt")

	os.WriteFile(file1, []byte("test"), 0644)
	os.WriteFile(file2, []byte("test"), 0644)

	tests := []struct {
		name          string
		input         string
		expectedCount int
		expectedQuery string
	}{
		{
			name:          "Single Windows path",
			input:         file1,
			expectedCount: 1,
			expectedQuery: "",
		},
		{
			name:          "Windows path with query before",
			input:         "Compare this file " + file1,
			expectedCount: 1,
			expectedQuery: "Compare this file",
		},
		{
			name:          "Windows path with query after",
			input:         file1 + " and tell me what it contains",
			expectedCount: 1,
			expectedQuery: "and tell me what it contains",
		},
		{
			name:          "Multiple Windows paths",
			input:         "Compare " + file1 + " and " + file2,
			expectedCount: 2,
			expectedQuery: "Compare and",
		},
		{
			name:          "Multiple paths with complex query",
			input:         "Compare these files " + file1 + " and " + file2 + " and tell me the differences",
			expectedCount: 2,
			expectedQuery: "Compare these files and and tell me the differences",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectAllPaths(tt.input)

			if len(result.Paths) != tt.expectedCount {
				t.Errorf("Expected %d paths, got %d", tt.expectedCount, len(result.Paths))
			}

			if result.Query != tt.expectedQuery {
				t.Errorf("Expected query '%s', got '%s'", tt.expectedQuery, result.Query)
			}

			// Verify all detected paths exist
			for i, path := range result.Paths {
				if !path.Exists {
					t.Errorf("Path %d should exist: %s", i, path.Path)
				}
			}
		})
	}
}

func TestDetectAllPaths_Unix(t *testing.T) {
	// Create temp files for testing (temp dir uses Unix paths on Unix systems)
	tempDir := t.TempDir()

	// Only run Unix tests on Unix systems
	if filepath.Separator != '/' {
		t.Skip("Skipping Unix path tests on Windows")
	}

	file1 := filepath.Join(tempDir, "file1.txt")
	file2 := filepath.Join(tempDir, "file2.txt")

	os.WriteFile(file1, []byte("test"), 0644)
	os.WriteFile(file2, []byte("test"), 0644)

	tests := []struct {
		name          string
		input         string
		expectedCount int
	}{
		{
			name:          "Single Unix path",
			input:         file1,
			expectedCount: 1,
		},
		{
			name:          "Multiple Unix paths",
			input:         "Compare " + file1 + " and " + file2,
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectAllPaths(tt.input)

			if len(result.Paths) != tt.expectedCount {
				t.Errorf("Expected %d paths, got %d", tt.expectedCount, len(result.Paths))
			}
		})
	}
}

func TestDetectAllPaths_NoPath(t *testing.T) {
	inputs := []string{
		"This is just regular text",
		"Tell me about programming",
		"https://example.com/path",  // URL, not file path
		"user@host:/path",            // SCP-style, not local path
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			result := DetectAllPaths(input)

			if result.HasPaths {
				t.Errorf("Should not detect paths in: %s", input)
			}

			if result.Query != input {
				t.Errorf("Query should be full input when no paths detected")
			}
		})
	}
}

func TestIsLikelyUnixPath(t *testing.T) {
	tests := []struct {
		text     string
		position int
		expected bool
	}{
		{"/home/user/file.txt", 0, true},
		{"/usr/local/bin", 0, true},
		{"/tmp/test", 0, true},
		{"http://example.com/path", 0, false},  // URL
		{"text /home/user", 5, true},           // Preceded by space
		{"text/path", 4, false},                // Not at word boundary
		{"/etc/config", 0, true},
		{"/Applications/App.app", 0, true},     // macOS
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			result := isLikelyUnixPath(tt.text, tt.position)
			if result != tt.expected {
				t.Errorf("isLikelyUnixPath(%q, %d) = %v, want %v", tt.text, tt.position, result, tt.expected)
			}
		})
	}
}

func TestDetectWindowsPaths(t *testing.T) {
	// Create temp file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "Valid Windows path",
			input:    testFile,
			expected: 1,
		},
		{
			name:     "Windows path with forward slashes",
			input:    filepath.ToSlash(testFile),
			expected: 1,
		},
		{
			name:     "Invalid drive letter",
			input:    "Z:\\nonexistent\\path",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := detectWindowsPaths(tt.input)
			if len(paths) != tt.expected {
				t.Errorf("Expected %d paths, got %d", tt.expected, len(paths))
			}
		})
	}
}

func TestExtractQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		paths    []pathDetectionResultInternal
		expected string
	}{
		{
			name:  "Query before path",
			input: "Compare this file C:\\test.txt",
			paths: []pathDetectionResultInternal{
				{StartIdx: 18, EndIdx: 29},
			},
			expected: "Compare this file",
		},
		{
			name:  "Query after path",
			input: "C:\\test.txt and analyze it",
			paths: []pathDetectionResultInternal{
				{StartIdx: 0, EndIdx: 11},
			},
			expected: "and analyze it",
		},
		{
			name:  "Query between paths",
			input: "C:\\file1.txt and C:\\file2.txt",
			paths: []pathDetectionResultInternal{
				{StartIdx: 0, EndIdx: 12},
				{StartIdx: 17, EndIdx: 29},
			},
			expected: "and",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractQuery(tt.input, tt.paths)
			if result != tt.expected {
				t.Errorf("Expected query '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
