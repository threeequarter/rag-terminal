package document

import (
	"os"
	"path/filepath"
	"regexp"
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

	// Windows absolute path patterns:
	// 1. Drive letter followed by colon and backslash
	// 2. May contain spaces, alphanumeric, dots, underscores, dashes
	// 3. May be quoted or unquoted

	// Pattern matches:
	// - C:\path\to\file
	// - "C:\path with spaces\file"
	// - D:\folder
	pathPattern := regexp.MustCompile(`(?:["]?)([A-Za-z]:\\(?:[^\\/:*?"<>|\r\n]+\\)*[^\\/:*?"<>|\r\n]*)(?:["]?)`)

	matches := pathPattern.FindAllStringSubmatch(input, -1)

	if len(matches) == 0 {
		return result
	}

	// Find the longest valid path (most specific)
	var bestMatch string
	var bestMatchIndex int
	var bestMatchLength int

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		candidatePath := strings.Trim(match[1], `"'`)

		// Check if path exists and is accessible
		if info, err := os.Stat(candidatePath); err == nil {
			// Valid path found
			matchIndex := strings.Index(input, match[0])
			if len(candidatePath) > bestMatchLength {
				bestMatch = candidatePath
				bestMatchIndex = matchIndex
				bestMatchLength = len(candidatePath)
				result.IsFile = !info.IsDir()
				result.IsDirectory = info.IsDir()
			}
		}
	}

	if bestMatch == "" {
		return result
	}

	// Extract queries before and after the path
	result.HasPath = true
	result.Path = bestMatch
	result.Exists = true

	// Find the actual position in the original input
	// Account for potential quotes around the path
	pathStartIdx := bestMatchIndex
	pathEndIdx := pathStartIdx + len(bestMatch)

	// Adjust for quotes if present
	if pathStartIdx > 0 && (input[pathStartIdx-1] == '"' || input[pathStartIdx-1] == '\'') {
		pathStartIdx--
	}
	if pathEndIdx < len(input) && (input[pathEndIdx] == '"' || input[pathEndIdx] == '\'') {
		pathEndIdx++
	}

	// Extract query before path
	if pathStartIdx > 0 {
		result.QueryBefore = strings.TrimSpace(input[:pathStartIdx])
	}

	// Extract query after path
	if pathEndIdx < len(input) {
		result.QueryAfter = strings.TrimSpace(input[pathEndIdx:])
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
