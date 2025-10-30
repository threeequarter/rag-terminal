package rag

import (
	"sort"
	"strings"

	"rag-terminal/internal/document"
	"rag-terminal/internal/logging"
	"rag-terminal/internal/vector"
)

// CodeChunkPrioritizer handles smart prioritization of code chunks
type CodeChunkPrioritizer struct{}

// PrioritizeCodeChunks applies structure-aware prioritization for code files
// Priority order:
// 1. Header chunks (0-2) containing procedure/function definitions
// 2. Semantically similar chunks (from vector search)
// 3. Surrounding context chunks
func (p *CodeChunkPrioritizer) PrioritizeCodeChunks(chunks []vector.DocumentChunk, maxChunks int) []vector.DocumentChunk {
	if len(chunks) == 0 {
		return chunks
	}

	// Check if this is a code file
	if !document.IsCodeFile(chunks[0].FilePath) {
		return chunks // Not code, return as-is
	}

	logging.Info("Applying smart chunk prioritization for code file: %s", chunks[0].FilePath)

	// Separate chunks into categories
	headerChunks := p.extractHeaderChunks(chunks)
	similarChunks := p.extractNonHeaderChunks(chunks)

	// Build prioritized list
	var prioritized []vector.DocumentChunk

	// 1. Always include header chunks (up to 3)
	headerLimit := 3
	if len(headerChunks) > headerLimit {
		headerChunks = headerChunks[:headerLimit]
	}
	prioritized = append(prioritized, headerChunks...)
	logging.Debug("Added %d header chunks", len(headerChunks))

	// 2. Add semantically similar chunks (those from vector search)
	remaining := maxChunks - len(prioritized)
	if remaining > 0 && len(similarChunks) > 0 {
		similarLimit := remaining
		if len(similarChunks) > similarLimit {
			similarChunks = similarChunks[:similarLimit]
		}
		prioritized = append(prioritized, similarChunks...)
		logging.Debug("Added %d semantically similar chunks", len(similarChunks))
	}

	// 3. Add surrounding context chunks if we still have room
	remaining = maxChunks - len(prioritized)
	if remaining > 0 {
		contextChunks := p.getSurroundingChunks(prioritized, chunks, remaining)
		if len(contextChunks) > 0 {
			prioritized = append(prioritized, contextChunks...)
			logging.Debug("Added %d surrounding context chunks", len(contextChunks))
		}
	}

	// Sort by chunk index to maintain document order
	sort.Slice(prioritized, func(i, j int) bool {
		return prioritized[i].ChunkIndex < prioritized[j].ChunkIndex
	})

	logging.Info("Final prioritization: %d total chunks (headers + similar + context)", len(prioritized))
	return prioritized
}

// extractHeaderChunks identifies chunks that likely contain headers/definitions
// For code files, these are typically the first few chunks (0-2)
func (p *CodeChunkPrioritizer) extractHeaderChunks(chunks []vector.DocumentChunk) []vector.DocumentChunk {
	var headers []vector.DocumentChunk

	for _, chunk := range chunks {
		// Check if this is one of the first few chunks (header region)
		if chunk.ChunkIndex <= 2 {
			// Additional heuristic: check if content contains definition keywords
			if p.isHeaderChunk(chunk) {
				headers = append(headers, chunk)
			}
		}
	}

	// Sort by chunk index
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].ChunkIndex < headers[j].ChunkIndex
	})

	return headers
}

// isHeaderChunk checks if a chunk contains header/definition content
func (p *CodeChunkPrioritizer) isHeaderChunk(chunk vector.DocumentChunk) bool {
	content := strings.ToUpper(chunk.Content)

	// SQL keywords
	sqlHeaders := []string{
		"CREATE PROCEDURE", "ALTER PROCEDURE",
		"CREATE FUNCTION", "ALTER FUNCTION",
		"CREATE TABLE", "ALTER TABLE",
		"@", "RETURNS", "AS BEGIN",
	}

	// Go keywords
	goHeaders := []string{
		"PACKAGE ", "FUNC ", "TYPE ", "INTERFACE ",
	}

	// Python keywords
	pythonHeaders := []string{
		"DEF ", "CLASS ", "IMPORT ", "FROM ",
	}

	// Java/C# keywords
	javaHeaders := []string{
		"PUBLIC CLASS", "PRIVATE CLASS", "INTERFACE ",
		"PUBLIC STATIC", "PACKAGE ", "NAMESPACE ",
	}

	allKeywords := append(sqlHeaders, goHeaders...)
	allKeywords = append(allKeywords, pythonHeaders...)
	allKeywords = append(allKeywords, javaHeaders...)

	for _, keyword := range allKeywords {
		if strings.Contains(content, keyword) {
			return true
		}
	}

	// Russian SQL comments (parameter definitions)
	if strings.Contains(content, "ИДЕНТИФИКАТОР") ||
		strings.Contains(content, "ПАРАМЕТР") ||
		strings.Contains(content, "РАСЧЕТ") {
		return true
	}

	return false
}

// extractNonHeaderChunks returns chunks that are not headers
func (p *CodeChunkPrioritizer) extractNonHeaderChunks(chunks []vector.DocumentChunk) []vector.DocumentChunk {
	var nonHeaders []vector.DocumentChunk

	for _, chunk := range chunks {
		if chunk.ChunkIndex > 2 || !p.isHeaderChunk(chunk) {
			nonHeaders = append(nonHeaders, chunk)
		}
	}

	return nonHeaders
}

// getSurroundingChunks finds chunks that are adjacent to already selected chunks
// This provides better context continuity
func (p *CodeChunkPrioritizer) getSurroundingChunks(
	selected []vector.DocumentChunk,
	allChunks []vector.DocumentChunk,
	maxToAdd int,
) []vector.DocumentChunk {
	if len(selected) == 0 || len(allChunks) == 0 {
		return nil
	}

	// Build set of already selected chunk indices
	selectedIndices := make(map[int]bool)
	for _, chunk := range selected {
		selectedIndices[chunk.ChunkIndex] = true
	}

	// Find chunks adjacent to selected ones
	type adjacentChunk struct {
		chunk    vector.DocumentChunk
		distance int // Distance to nearest selected chunk
	}

	var candidates []adjacentChunk

	for _, chunk := range allChunks {
		if selectedIndices[chunk.ChunkIndex] {
			continue // Already selected
		}

		// Calculate minimum distance to any selected chunk
		minDistance := 999999
		for _, sel := range selected {
			if sel.FilePath != chunk.FilePath {
				continue // Different file
			}

			distance := abs(chunk.ChunkIndex - sel.ChunkIndex)
			if distance < minDistance {
				minDistance = distance
			}
		}

		if minDistance <= 2 { // Only consider chunks within 2 positions
			candidates = append(candidates, adjacentChunk{
				chunk:    chunk,
				distance: minDistance,
			})
		}
	}

	// Sort by distance (closest first)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		return candidates[i].chunk.ChunkIndex < candidates[j].chunk.ChunkIndex
	})

	// Take up to maxToAdd
	var surrounding []vector.DocumentChunk
	for i := 0; i < len(candidates) && i < maxToAdd; i++ {
		surrounding = append(surrounding, candidates[i].chunk)
	}

	return surrounding
}

// abs returns absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
