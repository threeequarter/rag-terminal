package document

import (
	"strings"
	"unicode"
)

const (
	// DefaultChunkSize is the target size for each chunk in characters
	DefaultChunkSize = 1000

	// DefaultChunkOverlap is the number of characters to overlap between chunks
	// Reduced from 200 to 50 to maximize unique content in LLM context window
	DefaultChunkOverlap = 50

	// MaxChunkSize is the maximum allowed chunk size
	MaxChunkSize = 2000
)

// Chunker splits documents into overlapping chunks for embedding
type Chunker struct {
	ChunkSize    int
	ChunkOverlap int
	cleaner      *Cleaner
}

// NewChunker creates a new chunker with default settings
func NewChunker() *Chunker {
	return &Chunker{
		ChunkSize:    DefaultChunkSize,
		ChunkOverlap: DefaultChunkOverlap,
		cleaner:      NewCleaner(),
	}
}

// Chunk represents a single chunk of text
type Chunk struct {
	Content  string
	StartPos int
	EndPos   int
	Index    int
}

// ChunkDocument splits a document into overlapping chunks
// It attempts to preserve paragraph and sentence boundaries where possible
// For code files, it uses structure-aware chunking
func (c *Chunker) ChunkDocument(content string) []Chunk {
	// Check if content looks like code
	if c.isCodeContent(content) {
		return c.chunkAsCode(content)
	}

	if len(content) <= c.ChunkSize {
		// Document is small enough to be a single chunk
		return []Chunk{
			{
				Content:  content,
				StartPos: 0,
				EndPos:   len(content),
				Index:    0,
			},
		}
	}

	chunks := []Chunk{}
	position := 0
	chunkIndex := 0

	for position < len(content) {
		endPos := position + c.ChunkSize

		// Don't go past the end of the content
		if endPos > len(content) {
			endPos = len(content)
		}

		// Try to find a good break point (paragraph, sentence, or word boundary)
		if endPos < len(content) {
			endPos = c.findBreakPoint(content, position, endPos)
		}

		// Extract the chunk
		chunkContent := content[position:endPos]

		// Clean and normalize the chunk content
		chunkContent = c.cleaner.CleanText(chunkContent)

		// Skip chunks that are mostly whitespace or formatting
		if len(chunkContent) > 0 && !c.cleaner.IsContentMostlyWhitespace(chunkContent) {
			chunks = append(chunks, Chunk{
				Content:  chunkContent,
				StartPos: position,
				EndPos:   endPos,
				Index:    chunkIndex,
			})
			chunkIndex++
		}

		// Move position forward, accounting for overlap
		if endPos == len(content) {
			break
		}

		position = endPos - c.ChunkOverlap
		if position <= chunks[len(chunks)-1].StartPos {
			// Ensure we're making progress
			position = chunks[len(chunks)-1].StartPos + 1
		}
	}

	return chunks
}

// findBreakPoint attempts to find a natural break point near the target end position
func (c *Chunker) findBreakPoint(content string, start, targetEnd int) int {
	// Search window: look backwards up to 20% of chunk size for a break point
	searchStart := targetEnd - (c.ChunkSize / 5)
	if searchStart < start {
		searchStart = start
	}

	// Priority 1: Find paragraph break (double newline)
	if pos := c.findLastOccurrence(content, searchStart, targetEnd, "\n\n"); pos != -1 {
		return pos + 2 // Include the double newline
	}

	// Priority 2: Find single newline
	if pos := c.findLastOccurrence(content, searchStart, targetEnd, "\n"); pos != -1 {
		return pos + 1 // Include the newline
	}

	// Priority 3: Find sentence end (. ! ?)
	if pos := c.findLastSentenceEnd(content, searchStart, targetEnd); pos != -1 {
		return pos
	}

	// Priority 4: Find word boundary (space)
	if pos := c.findLastOccurrence(content, searchStart, targetEnd, " "); pos != -1 {
		return pos + 1 // Move past the space
	}

	// Priority 5: Find any whitespace
	for i := targetEnd - 1; i >= searchStart; i-- {
		if unicode.IsSpace(rune(content[i])) {
			return i + 1
		}
	}

	// No good break point found, use target end
	return targetEnd
}

// findLastOccurrence finds the last occurrence of a substring in a range
func (c *Chunker) findLastOccurrence(content string, start, end int, substr string) int {
	searchContent := content[start:end]
	lastIdx := strings.LastIndex(searchContent, substr)
	if lastIdx != -1 {
		return start + lastIdx
	}
	return -1
}

// findLastSentenceEnd finds the last sentence-ending punctuation in a range
func (c *Chunker) findLastSentenceEnd(content string, start, end int) int {
	for i := end - 1; i >= start; i-- {
		if content[i] == '.' || content[i] == '!' || content[i] == '?' {
			// Check if followed by space or newline (actual sentence end)
			if i+1 < len(content) {
				next := content[i+1]
				if unicode.IsSpace(rune(next)) || next == '\n' || next == '\r' {
					return i + 1
				}
			} else {
				// End of content
				return i + 1
			}
		}
	}
	return -1
}

// GetChunkWithContext returns a chunk with some surrounding context for better embedding
func (c *Chunker) GetChunkWithContext(content string, chunk Chunk, contextSize int) string {
	// Add context before
	contextStart := chunk.StartPos - contextSize
	if contextStart < 0 {
		contextStart = 0
	}

	// Add context after
	contextEnd := chunk.EndPos + contextSize
	if contextEnd > len(content) {
		contextEnd = len(content)
	}

	beforeContext := ""
	if contextStart < chunk.StartPos {
		beforeContext = content[contextStart:chunk.StartPos]
		beforeContext = strings.TrimSpace(beforeContext)
		if beforeContext != "" {
			beforeContext = "..." + beforeContext + " "
		}
	}

	afterContext := ""
	if contextEnd > chunk.EndPos {
		afterContext = content[chunk.EndPos:contextEnd]
		afterContext = strings.TrimSpace(afterContext)
		if afterContext != "" {
			afterContext = " " + afterContext + "..."
		}
	}

	return beforeContext + chunk.Content + afterContext
}

// isCodeContent detects if content is source code
func (c *Chunker) isCodeContent(content string) bool {
	// Check for common code patterns
	codeIndicators := []string{
		"func ", "function ", "def ", "class ", "import ",
		"package ", "public ", "private ", "const ",
		"var ", "let ", "interface ", "struct ", "impl ",
		"#include", "using ", "namespace ",
	}

	indicatorCount := 0
	for _, indicator := range codeIndicators {
		if strings.Contains(content, indicator) {
			indicatorCount++
		}
	}

	// If we find 3+ code indicators, likely code
	if indicatorCount >= 3 {
		return true
	}

	// Check for code-like structure (lots of braces, semicolons)
	braceCount := strings.Count(content, "{") + strings.Count(content, "}")
	semicolonCount := strings.Count(content, ";")
	totalLines := strings.Count(content, "\n") + 1

	// If more than 20% of lines have braces or semicolons, likely code
	codeChars := braceCount + semicolonCount
	if totalLines > 10 && float64(codeChars)/float64(totalLines) > 0.2 {
		return true
	}

	return false
}

// chunkAsCode uses code-aware chunking
func (c *Chunker) chunkAsCode(content string) []Chunk {
	codeChunker := NewCodeChunker("")
	return codeChunker.ChunkCode(content, c.ChunkSize)
}
