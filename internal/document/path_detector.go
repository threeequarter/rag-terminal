package document

import (
	"os"
	"path/filepath"
	"strings"
)

// PathDetectionResult holds the result of path detection in user input
type PathDetectionResult struct {
	HasPath      bool   // Whether a valid path was detected
	Path         string // The detected file or folder path
	QueryBefore  string // Text before the path
	QueryAfter   string // Text after the path
	IsFile       bool   // True if path points to a file
	IsDirectory  bool   // True if path points to a directory
	Exists       bool   // Whether the path exists on filesystem
}

// DetectPath attempts to extract a file or folder path from user input
// Supports patterns:
// - "C:\path\to\file.txt"
// - "C:\path\to\folder"
// - 'C:\temp count files'           (path + query)
// - 'Count files in C:\temp'        (query + path)
// - 'Count files in C:\temp total'  (query + path + query)
func DetectPath(input string) PathDetectionResult {
	result := PathDetectionResult{
		HasPath: false,
	}

	// Try to find Windows absolute paths by looking for drive letter patterns
	// Split input into tokens and try to build paths
	var candidatePaths []struct {
		path      string
		startIdx  int
		endIdx    int
		info      os.FileInfo
	}

	// Look for drive letter pattern (C:, D:, etc.)
	for i := 0; i < len(input)-2; i++ {
		// Check for drive letter pattern: [A-Z]:
		if (input[i] >= 'A' && input[i] <= 'Z' || input[i] >= 'a' && input[i] <= 'z') &&
			input[i+1] == ':' && (i+2 < len(input) && input[i+2] == '\\') {

			// Found potential start of Windows path
			// Extract path by reading until whitespace or end
			pathStart := i
			pathEnd := pathStart + 3 // Start after C:\

			// Read until we hit something that definitely can't be a path
			// We'll be greedy and read everything, then trim back
			for pathEnd < len(input) {
				ch := input[pathEnd]
				// Stop at characters that are definitely not in Windows paths
				if ch == '\r' || ch == '\n' || ch == '\t' {
					break
				}
				pathEnd++
			}

			candidatePath := input[pathStart:pathEnd]

			// Try progressively shorter paths starting from the longest
			// This handles cases like "C:\path\to\file.txt extra text"
			for len(candidatePath) > 3 {
				// Clean up trailing whitespace and backslashes
				candidatePath = strings.TrimRight(candidatePath, " \t\\")

				// Skip if it ends with invalid path characters
				if len(candidatePath) > 0 {
					lastChar := candidatePath[len(candidatePath)-1]
					if lastChar == '<' || lastChar == '>' || lastChar == '|' ||
						lastChar == '"' || lastChar == '*' || lastChar == '?' || lastChar == ':' {
						// Remove this character and try again
						candidatePath = candidatePath[:len(candidatePath)-1]
						continue
					}
				}

				// Test if this path exists
				if info, err := os.Stat(candidatePath); err == nil {
					candidatePaths = append(candidatePaths, struct {
						path      string
						startIdx  int
						endIdx    int
						info      os.FileInfo
					}{
						path:     candidatePath,
						startIdx: pathStart,
						endIdx:   pathStart + len(candidatePath),
						info:     info,
					})
					break
				}

				// Try removing last token (space-separated word or path component)
				// First try removing after last space
				if lastSpace := strings.LastIndex(candidatePath, " "); lastSpace > 3 {
					candidatePath = candidatePath[:lastSpace]
					continue
				}

				// Then try removing last path component
				lastBackslash := strings.LastIndex(candidatePath, "\\")
				if lastBackslash <= 2 { // Don't go before "C:\"
					break
				}
				candidatePath = candidatePath[:lastBackslash]
			}
		}
	}

	// Pick the longest valid path
	var bestCandidate *struct {
		path      string
		startIdx  int
		endIdx    int
		info      os.FileInfo
	}

	for i := range candidatePaths {
		if bestCandidate == nil || len(candidatePaths[i].path) > len(bestCandidate.path) {
			bestCandidate = &candidatePaths[i]
		}
	}

	if bestCandidate == nil {
		return result
	}

	// Found a valid path
	result.HasPath = true
	result.Path = bestCandidate.path
	result.Exists = true
	result.IsFile = !bestCandidate.info.IsDir()
	result.IsDirectory = bestCandidate.info.IsDir()

	// Extract queries before and after the path
	if bestCandidate.startIdx > 0 {
		result.QueryBefore = strings.TrimSpace(input[:bestCandidate.startIdx])
	}

	if bestCandidate.endIdx < len(input) {
		result.QueryAfter = strings.TrimSpace(input[bestCandidate.endIdx:])
	}

	return result
}

// GetFullQuery combines before and after queries into a single query string
func (r PathDetectionResult) GetFullQuery() string {
	parts := []string{}

	if r.QueryBefore != "" {
		parts = append(parts, r.QueryBefore)
	}

	if r.QueryAfter != "" {
		parts = append(parts, r.QueryAfter)
	}

	return strings.TrimSpace(strings.Join(parts, " "))
}

// GetAbsolutePath returns the absolute path, resolving any relative components
func (r PathDetectionResult) GetAbsolutePath() (string, error) {
	if !r.HasPath {
		return "", nil
	}
	return filepath.Abs(r.Path)
}

// ShouldProcessPath returns true if the path should be processed
// (exists and is either a file or directory)
func (r PathDetectionResult) ShouldProcessPath() bool {
	return r.HasPath && r.Exists && (r.IsFile || r.IsDirectory)
}
