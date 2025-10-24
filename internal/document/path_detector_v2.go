package document

import (
	"os"
	"strings"
)

// MultiPathDetectionResult holds multiple detected paths
type MultiPathDetectionResult struct {
	HasPaths bool                  // Whether any valid paths were detected
	Paths    []PathDetectionResult // All detected paths
	Query    string                // Remaining query text (without paths)
}

// PathDetectionResultV2 holds the result of path detection in user input
type PathDetectionResultV2 struct {
	HasPath      bool   // Whether a valid path was detected
	Path         string // The detected file or folder path
	StartIdx     int    // Start position in original string
	EndIdx       int    // End position in original string
	IsFile       bool   // True if path points to a file
	IsDirectory  bool   // True if path points to a directory
	Exists       bool   // Whether the path exists on filesystem
}

// DetectAllPaths detects all file/folder paths in user input (Windows, Linux, macOS)
func DetectAllPaths(input string) MultiPathDetectionResult {
	result := MultiPathDetectionResult{
		HasPaths: false,
		Paths:    []PathDetectionResult{},
	}

	var detectedPaths []PathDetectionResultV2

	// Detect Windows paths (C:\, D:\, etc.)
	windowsPaths := detectWindowsPaths(input)
	detectedPaths = append(detectedPaths, windowsPaths...)

	// Detect Unix paths (/home/, /usr/, etc.)
	unixPaths := detectUnixPaths(input)
	detectedPaths = append(detectedPaths, unixPaths...)

	// If no paths found, return empty result
	if len(detectedPaths) == 0 {
		result.Query = input
		return result
	}

	// Sort paths by start position
	sortPathsByPosition(detectedPaths)

	// Convert to PathDetectionResult and extract query
	result.HasPaths = true
	result.Query = extractQuery(input, detectedPaths)

	for _, path := range detectedPaths {
		result.Paths = append(result.Paths, PathDetectionResult{
			HasPath:     true,
			Path:        path.Path,
			IsFile:      path.IsFile,
			IsDirectory: path.IsDirectory,
			Exists:      path.Exists,
		})
	}

	return result
}

// detectWindowsPaths finds Windows-style paths (C:\, D:\, etc.)
func detectWindowsPaths(input string) []PathDetectionResultV2 {
	var paths []PathDetectionResultV2

	for i := 0; i < len(input)-2; i++ {
		// Check for drive letter pattern: [A-Z]:\ or [A-Z]:/
		if (input[i] >= 'A' && input[i] <= 'Z' || input[i] >= 'a' && input[i] <= 'z') &&
			input[i+1] == ':' && (i+2 < len(input) && (input[i+2] == '\\' || input[i+2] == '/')) {

			pathStart := i
			pathEnd := pathStart + 3

			// Read until we hit something that definitely can't be a path
			for pathEnd < len(input) {
				ch := input[pathEnd]
				if ch == '\r' || ch == '\n' || ch == '\t' {
					break
				}
				pathEnd++
			}

			candidatePath := input[pathStart:pathEnd]

			// Try progressively shorter paths
			for len(candidatePath) > 3 {
				candidatePath = strings.TrimRight(candidatePath, " \t\\/")

				// Skip if ends with invalid characters
				if len(candidatePath) > 0 {
					lastChar := candidatePath[len(candidatePath)-1]
					if lastChar == '<' || lastChar == '>' || lastChar == '|' ||
						lastChar == '"' || lastChar == '*' || lastChar == '?' || lastChar == ':' {
						candidatePath = candidatePath[:len(candidatePath)-1]
						continue
					}
				}

				// Test if this path exists
				if info, err := os.Stat(candidatePath); err == nil {
					paths = append(paths, PathDetectionResultV2{
						HasPath:     true,
						Path:        candidatePath,
						StartIdx:    pathStart,
						EndIdx:      pathStart + len(candidatePath),
						IsFile:      !info.IsDir(),
						IsDirectory: info.IsDir(),
						Exists:      true,
					})
					break
				}

				// Try removing last component
				if lastSpace := strings.LastIndex(candidatePath, " "); lastSpace > 3 {
					candidatePath = candidatePath[:lastSpace]
					continue
				}

				lastSep := strings.LastIndexAny(candidatePath, "\\/")
				if lastSep <= 2 {
					break
				}
				candidatePath = candidatePath[:lastSep]
			}

			// Skip past this path in the search
			if len(paths) > 0 && paths[len(paths)-1].StartIdx == pathStart {
				i = paths[len(paths)-1].EndIdx - 1
			}
		}
	}

	return paths
}

// detectUnixPaths finds Unix-style paths (/home/, /usr/, etc.)
func detectUnixPaths(input string) []PathDetectionResultV2 {
	var paths []PathDetectionResultV2

	for i := 0; i < len(input); i++ {
		// Check for absolute Unix path: starts with /
		if input[i] == '/' {
			// Make sure it's not just a slash in a URL or something
			// Unix paths typically start with /home, /usr, /var, /tmp, /etc, /opt, etc.
			if !isLikelyUnixPath(input, i) {
				continue
			}

			pathStart := i
			pathEnd := pathStart + 1

			// Read until whitespace or end
			for pathEnd < len(input) {
				ch := input[pathEnd]
				if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == ',' || ch == ';' {
					break
				}
				pathEnd++
			}

			candidatePath := input[pathStart:pathEnd]

			// Try progressively shorter paths
			for len(candidatePath) > 1 {
				candidatePath = strings.TrimRight(candidatePath, " \t/")

				// Skip if ends with invalid characters
				if len(candidatePath) > 0 {
					lastChar := candidatePath[len(candidatePath)-1]
					if lastChar == '<' || lastChar == '>' || lastChar == '|' ||
						lastChar == '"' || lastChar == '*' || lastChar == '?' {
						candidatePath = candidatePath[:len(candidatePath)-1]
						continue
					}
				}

				// Test if this path exists
				if info, err := os.Stat(candidatePath); err == nil {
					paths = append(paths, PathDetectionResultV2{
						HasPath:     true,
						Path:        candidatePath,
						StartIdx:    pathStart,
						EndIdx:      pathStart + len(candidatePath),
						IsFile:      !info.IsDir(),
						IsDirectory: info.IsDir(),
						Exists:      true,
					})
					break
				}

				// Try removing last component
				if lastSpace := strings.LastIndex(candidatePath, " "); lastSpace > 1 {
					candidatePath = candidatePath[:lastSpace]
					continue
				}

				lastSep := strings.LastIndex(candidatePath, "/")
				if lastSep <= 0 {
					break
				}
				candidatePath = candidatePath[:lastSep]
			}

			// Skip past this path in the search
			if len(paths) > 0 && paths[len(paths)-1].StartIdx == pathStart {
				i = paths[len(paths)-1].EndIdx - 1
			}
		}
	}

	return paths
}

// isLikelyUnixPath checks if a slash at position i is likely the start of a Unix path
func isLikelyUnixPath(input string, i int) bool {
	// Check if preceded by whitespace or at start
	if i > 0 {
		prev := input[i-1]
		if prev != ' ' && prev != '\t' && prev != '\n' && prev != ',' && prev != '(' && prev != '[' {
			return false
		}
	}

	// Check for common Unix path prefixes
	commonPrefixes := []string{
		"/home/", "/usr/", "/var/", "/tmp/", "/etc/", "/opt/",
		"/bin/", "/sbin/", "/lib/", "/mnt/", "/media/", "/root/",
		"/Applications/", "/Users/", "/System/", "/Library/", // macOS
	}

	remaining := input[i:]
	for _, prefix := range commonPrefixes {
		if strings.HasPrefix(remaining, prefix) {
			return true
		}
	}

	// If it has multiple slashes, likely a path
	if strings.Count(remaining[:minInt(len(remaining), 50)], "/") >= 2 {
		return true
	}

	return false
}

// sortPathsByPosition sorts paths by their start position
func sortPathsByPosition(paths []PathDetectionResultV2) {
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[j].StartIdx < paths[i].StartIdx {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
}

// extractQuery extracts the query text by removing detected paths
func extractQuery(input string, paths []PathDetectionResultV2) string {
	if len(paths) == 0 {
		return input
	}

	// Build query by removing path segments
	var queryParts []string
	lastEnd := 0

	for _, path := range paths {
		// Add text before this path
		if path.StartIdx > lastEnd {
			part := strings.TrimSpace(input[lastEnd:path.StartIdx])
			if part != "" {
				queryParts = append(queryParts, part)
			}
		}
		lastEnd = path.EndIdx
	}

	// Add text after last path
	if lastEnd < len(input) {
		part := strings.TrimSpace(input[lastEnd:])
		if part != "" {
			queryParts = append(queryParts, part)
		}
	}

	return strings.Join(queryParts, " ")
}

// minInt returns the minimum of two integers
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
