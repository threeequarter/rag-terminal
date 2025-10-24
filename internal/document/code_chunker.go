package document

import (
	"regexp"
	"strings"
)

// CodeChunker handles chunking of source code files
type CodeChunker struct {
	language string
}

// NewCodeChunker creates a new code-aware chunker
func NewCodeChunker(language string) *CodeChunker {
	return &CodeChunker{
		language: language,
	}
}

// CodeBlock represents a logical code structure
type CodeBlock struct {
	Type      string // "function", "class", "struct", "interface", "method"
	Name      string
	Content   string
	StartLine int
	EndLine   int
	Language  string
}

// ChunkCode splits code into logical blocks (functions, classes, etc.)
func (c *CodeChunker) ChunkCode(code string, maxChunkSize int) []Chunk {
	// Detect language if not set
	if c.language == "" {
		c.language = c.detectLanguage(code)
	}

	// Extract code blocks based on language
	blocks := c.extractCodeBlocks(code)

	// Convert code blocks to chunks
	chunks := make([]Chunk, 0)
	chunkIndex := 0

	for _, block := range blocks {
		content := c.optimizeCodeBlock(block)

		// If block is too large, split it but keep it meaningful
		if len(content) > maxChunkSize {
			subChunks := c.splitLargeBlock(block, maxChunkSize)
			for _, subContent := range subChunks {
				chunks = append(chunks, Chunk{
					Content:  subContent,
					StartPos: block.StartLine,
					EndPos:   block.EndLine,
					Index:    chunkIndex,
				})
				chunkIndex++
			}
		} else {
			chunks = append(chunks, Chunk{
				Content:  content,
				StartPos: block.StartLine,
				EndPos:   block.EndLine,
				Index:    chunkIndex,
			})
			chunkIndex++
		}
	}

	return chunks
}

// detectLanguage attempts to detect programming language from code
func (c *CodeChunker) detectLanguage(code string) string {
	// Check for language-specific patterns
	patterns := map[string][]string{
		"go":         {"func ", "package ", "import ", "type ", "interface "},
		"python":     {"def ", "class ", "import ", "from ", "__init__"},
		"javascript": {"function ", "const ", "let ", "var ", "=>"},
		"typescript": {"interface ", "type ", "const ", "function ", "=>"},
		"java":       {"public class", "private ", "public ", "void ", "import "},
		"csharp":     {"public class", "private ", "using ", "namespace "},
		"rust":       {"fn ", "impl ", "struct ", "enum ", "use "},
		"cpp":        {"#include", "class ", "void ", "int ", "namespace "},
	}

	scores := make(map[string]int)
	for lang, keywords := range patterns {
		for _, keyword := range keywords {
			if strings.Contains(code, keyword) {
				scores[lang]++
			}
		}
	}

	// Find language with highest score
	maxScore := 0
	detectedLang := "generic"
	for lang, score := range scores {
		if score > maxScore {
			maxScore = score
			detectedLang = lang
		}
	}

	return detectedLang
}

// extractCodeBlocks extracts logical code structures
func (c *CodeChunker) extractCodeBlocks(code string) []CodeBlock {
	switch c.language {
	case "go":
		return c.extractGoBlocks(code)
	case "python":
		return c.extractPythonBlocks(code)
	case "javascript", "typescript":
		return c.extractJavaScriptBlocks(code)
	case "java", "csharp":
		return c.extractJavaLikeBlocks(code)
	case "rust":
		return c.extractRustBlocks(code)
	default:
		return c.extractGenericBlocks(code)
	}
}

// extractGoBlocks extracts Go functions, methods, types
func (c *CodeChunker) extractGoBlocks(code string) []CodeBlock {
	blocks := []CodeBlock{}
	lines := strings.Split(code, "\n")

	// Regex patterns for Go structures
	funcPattern := regexp.MustCompile(`^func\s+(\w+)`)
	methodPattern := regexp.MustCompile(`^func\s+\([^)]+\)\s+(\w+)`)
	typePattern := regexp.MustCompile(`^type\s+(\w+)\s+(struct|interface)`)

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// Match function
		if match := funcPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "function", match[1])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		// Match method
		if match := methodPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "method", match[1])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		// Match type
		if match := typePattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, match[2], match[1])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		i++
	}

	return blocks
}

// extractPythonBlocks extracts Python functions and classes
func (c *CodeChunker) extractPythonBlocks(code string) []CodeBlock {
	blocks := []CodeBlock{}
	lines := strings.Split(code, "\n")

	funcPattern := regexp.MustCompile(`^def\s+(\w+)`)
	classPattern := regexp.MustCompile(`^class\s+(\w+)`)

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// Match function
		if match := funcPattern.FindStringSubmatch(line); match != nil {
			block := c.extractIndentedBlock(lines, i, "function", match[1])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		// Match class
		if match := classPattern.FindStringSubmatch(line); match != nil {
			block := c.extractIndentedBlock(lines, i, "class", match[1])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		i++
	}

	return blocks
}

// extractJavaScriptBlocks extracts JavaScript/TypeScript functions and classes
func (c *CodeChunker) extractJavaScriptBlocks(code string) []CodeBlock {
	blocks := []CodeBlock{}
	lines := strings.Split(code, "\n")

	funcPattern := regexp.MustCompile(`^(function|const|let|var)\s+(\w+)\s*[=\(]`)
	classPattern := regexp.MustCompile(`^class\s+(\w+)`)
	arrowFuncPattern := regexp.MustCompile(`^(const|let|var)\s+(\w+)\s*=\s*\([^)]*\)\s*=>`)

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// Match class
		if match := classPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "class", match[1])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		// Match arrow function
		if match := arrowFuncPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "function", match[2])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		// Match regular function
		if match := funcPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "function", match[2])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		i++
	}

	return blocks
}

// extractJavaLikeBlocks extracts Java/C# classes and methods
func (c *CodeChunker) extractJavaLikeBlocks(code string) []CodeBlock {
	blocks := []CodeBlock{}
	lines := strings.Split(code, "\n")

	classPattern := regexp.MustCompile(`^(public|private|protected)?\s*(class|interface)\s+(\w+)`)
	methodPattern := regexp.MustCompile(`^(public|private|protected)?\s*\w+\s+(\w+)\s*\(`)

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// Match class
		if match := classPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, match[2], match[3])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		// Match method
		if match := methodPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "method", match[2])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		i++
	}

	return blocks
}

// extractRustBlocks extracts Rust functions, structs, impls
func (c *CodeChunker) extractRustBlocks(code string) []CodeBlock {
	blocks := []CodeBlock{}
	lines := strings.Split(code, "\n")

	funcPattern := regexp.MustCompile(`^(pub\s+)?fn\s+(\w+)`)
	structPattern := regexp.MustCompile(`^(pub\s+)?struct\s+(\w+)`)
	implPattern := regexp.MustCompile(`^impl\s+(\w+)`)

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// Match function
		if match := funcPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "function", match[2])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		// Match struct
		if match := structPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "struct", match[2])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		// Match impl
		if match := implPattern.FindStringSubmatch(line); match != nil {
			block := c.extractBracedBlock(lines, i, "impl", match[1])
			blocks = append(blocks, block)
			i = block.EndLine
			continue
		}

		i++
	}

	return blocks
}

// extractGenericBlocks fallback for unknown languages
func (c *CodeChunker) extractGenericBlocks(code string) []CodeBlock {
	// Split by blank lines or every N lines
	blocks := []CodeBlock{}
	lines := strings.Split(code, "\n")

	currentBlock := []string{}
	startLine := 0

	for i, line := range lines {
		if strings.TrimSpace(line) == "" && len(currentBlock) > 0 {
			// End of block
			blocks = append(blocks, CodeBlock{
				Type:      "block",
				Name:      "",
				Content:   strings.Join(currentBlock, "\n"),
				StartLine: startLine,
				EndLine:   i,
				Language:  "generic",
			})
			currentBlock = []string{}
			startLine = i + 1
		} else if strings.TrimSpace(line) != "" {
			currentBlock = append(currentBlock, line)
		}
	}

	// Add remaining block
	if len(currentBlock) > 0 {
		blocks = append(blocks, CodeBlock{
			Type:      "block",
			Name:      "",
			Content:   strings.Join(currentBlock, "\n"),
			StartLine: startLine,
			EndLine:   len(lines) - 1,
			Language:  "generic",
		})
	}

	return blocks
}

// extractBracedBlock extracts a block delimited by braces {}
func (c *CodeChunker) extractBracedBlock(lines []string, startIdx int, blockType string, name string) CodeBlock {
	braceCount := 0
	foundStart := false
	content := []string{}

	for i := startIdx; i < len(lines); i++ {
		line := lines[i]
		content = append(content, line)

		for _, ch := range line {
			if ch == '{' {
				braceCount++
				foundStart = true
			} else if ch == '}' {
				braceCount--
			}
		}

		// Block complete when braces are balanced
		if foundStart && braceCount == 0 {
			return CodeBlock{
				Type:      blockType,
				Name:      name,
				Content:   strings.Join(content, "\n"),
				StartLine: startIdx,
				EndLine:   i + 1,
				Language:  c.language,
			}
		}
	}

	// If we didn't find closing brace, return what we have
	return CodeBlock{
		Type:      blockType,
		Name:      name,
		Content:   strings.Join(content, "\n"),
		StartLine: startIdx,
		EndLine:   len(lines),
		Language:  c.language,
	}
}

// extractIndentedBlock extracts a Python-style indented block
func (c *CodeChunker) extractIndentedBlock(lines []string, startIdx int, blockType string, name string) CodeBlock {
	content := []string{lines[startIdx]}
	baseIndent := c.getIndentLevel(lines[startIdx])

	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]

		// Empty lines are part of the block
		if strings.TrimSpace(line) == "" {
			content = append(content, line)
			continue
		}

		currentIndent := c.getIndentLevel(line)

		// If indent level is back to or less than base, block is done
		if currentIndent <= baseIndent {
			return CodeBlock{
				Type:      blockType,
				Name:      name,
				Content:   strings.Join(content, "\n"),
				StartLine: startIdx,
				EndLine:   i,
				Language:  c.language,
			}
		}

		content = append(content, line)
	}

	return CodeBlock{
		Type:      blockType,
		Name:      name,
		Content:   strings.Join(content, "\n"),
		StartLine: startIdx,
		EndLine:   len(lines),
		Language:  c.language,
	}
}

// getIndentLevel returns the indentation level of a line
func (c *CodeChunker) getIndentLevel(line string) int {
	indent := 0
	for _, ch := range line {
		if ch == ' ' {
			indent++
		} else if ch == '\t' {
			indent += 4
		} else {
			break
		}
	}
	return indent
}

// optimizeCodeBlock removes unnecessary whitespace and comments while preserving structure
func (c *CodeChunker) optimizeCodeBlock(block CodeBlock) string {
	lines := strings.Split(block.Content, "\n")
	optimized := []string{}

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")

		// Skip completely empty lines (but keep one for readability)
		if trimmed == "" {
			if len(optimized) > 0 && optimized[len(optimized)-1] != "" {
				optimized = append(optimized, "")
			}
			continue
		}

		// Keep the line (we don't remove comments in code - they're often valuable)
		optimized = append(optimized, trimmed)
	}

	return strings.Join(optimized, "\n")
}

// splitLargeBlock splits a large code block into smaller chunks at logical boundaries
func (c *CodeChunker) splitLargeBlock(block CodeBlock, maxSize int) []string {
	lines := strings.Split(block.Content, "\n")
	chunks := []string{}
	currentChunk := []string{}
	currentSize := 0

	for _, line := range lines {
		lineSize := len(line) + 1 // +1 for newline

		// If adding this line would exceed max size and we have content, save chunk
		if currentSize+lineSize > maxSize && len(currentChunk) > 0 {
			chunks = append(chunks, strings.Join(currentChunk, "\n"))
			currentChunk = []string{}
			currentSize = 0
		}

		currentChunk = append(currentChunk, line)
		currentSize += lineSize
	}

	// Add remaining chunk
	if len(currentChunk) > 0 {
		chunks = append(chunks, strings.Join(currentChunk, "\n"))
	}

	return chunks
}

// IsCodeFile determines if a file is likely source code based on extension
func IsCodeFile(filename string) bool {
	codeExtensions := map[string]bool{
		".go":   true, ".py":   true, ".js":   true, ".ts":   true,
		".java": true, ".c":    true, ".cpp":  true, ".h":    true,
		".rs":   true, ".cs":   true, ".php":  true, ".rb":   true,
		".swift": true, ".kt":  true, ".scala": true, ".m":   true,
		".sh":   true, ".bash": true, ".sql":  true, ".r":    true,
		".jsx":  true, ".tsx":  true, ".vue":  true,
	}

	ext := strings.ToLower(filename)
	for extension := range codeExtensions {
		if strings.HasSuffix(ext, extension) {
			return true
		}
	}
	return false
}
