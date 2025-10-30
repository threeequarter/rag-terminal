package document

import (
	"path/filepath"
	"strings"
)

// CodeExtractor handles code-aware excerpt extraction for programming languages
type CodeExtractor struct {
	fileType string
}

// ExcerptBlock represents a logical block for excerpt extraction
type ExcerptBlock struct {
	Content   string
	Score     float64
	Position  int
	BlockType string // "header", "procedure", "statement", "block", "comment"
}

// NewCodeExtractor creates a code-aware extractor based on file path
func NewCodeExtractor(filePath string) *CodeExtractor {
	ext := strings.ToLower(filepath.Ext(filePath))
	return &CodeExtractor{
		fileType: detectFileType(ext),
	}
}

// detectFileType maps file extension to language type
func detectFileType(ext string) string {
	switch ext {
	case ".sql":
		return "sql"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".cs":
		return "csharp"
	case ".js", ".ts":
		return "javascript"
	default:
		return "generic"
	}
}

// ExtractCodeExcerpt extracts relevant code excerpt using syntax-aware logic
func (ce *CodeExtractor) ExtractCodeExcerpt(content string, query string, maxLength int) string {
	if len(content) <= maxLength {
		return content
	}

	switch ce.fileType {
	case "sql":
		return ce.extractSQLExcerpt(content, query, maxLength)
	default:
		return ce.extractGenericCodeExcerpt(content, query, maxLength)
	}
}

// extractSQLExcerpt extracts SQL code with syntax-aware boundaries
func (ce *CodeExtractor) extractSQLExcerpt(content string, query string, maxLength int) string {
	blocks := ce.parseSQLBlocks(content)

	if len(blocks) == 0 {
		return ce.truncateAtSQLBoundary(content, maxLength)
	}

	// Score blocks based on relevance and importance
	scoredBlocks := ce.scoreSQLBlocks(blocks, query)

	// Select top blocks until we hit maxLength
	selectedBlocks := ce.selectTopBlocks(scoredBlocks, maxLength)

	if len(selectedBlocks) == 0 {
		return ce.truncateAtSQLBoundary(content, maxLength)
	}

	// Build result preserving original order
	return ce.buildCodeResult(selectedBlocks, len(blocks))
}

// parseSQLBlocks parses SQL content into logical blocks
func (ce *CodeExtractor) parseSQLBlocks(content string) []ExcerptBlock {
	var blocks []ExcerptBlock
	lines := strings.Split(content, "\n")

	var currentBlock strings.Builder
	var blockType string
	blockPosition := 0
	inProcedure := false
	inBlock := false
	blockDepth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmedUpper := strings.ToUpper(trimmed)

		// Detect stored procedure header
		if strings.HasPrefix(trimmedUpper, "CREATE PROCEDURE") ||
		   strings.HasPrefix(trimmedUpper, "ALTER PROCEDURE") ||
		   strings.HasPrefix(trimmedUpper, "CREATE FUNCTION") ||
		   strings.HasPrefix(trimmedUpper, "ALTER FUNCTION") {
			// Flush previous block
			if currentBlock.Len() > 0 {
				blocks = append(blocks, ExcerptBlock{
					Content:   strings.TrimSpace(currentBlock.String()),
					Position:  blockPosition,
					BlockType: blockType,
				})
				currentBlock.Reset()
			}

			inProcedure = true
			blockType = "procedure_header"
			blockPosition = len(blocks)
			currentBlock.WriteString(line)
			currentBlock.WriteString("\n")
			continue
		}

		// Detect AS keyword (end of procedure header)
		if inProcedure && blockType == "procedure_header" && trimmedUpper == "AS" {
			currentBlock.WriteString(line)
			currentBlock.WriteString("\n")

			// Save procedure header as high-priority block
			blocks = append(blocks, ExcerptBlock{
				Content:   strings.TrimSpace(currentBlock.String()),
				Position:  blockPosition,
				BlockType: "procedure_header",
			})

			currentBlock.Reset()
			blockType = "statement"
			blockPosition = len(blocks)
			continue
		}

		// Track BEGIN/END blocks
		if strings.Contains(trimmedUpper, "BEGIN") {
			if currentBlock.Len() > 0 && blockDepth == 0 {
				// Flush previous statement
				blocks = append(blocks, ExcerptBlock{
					Content:   strings.TrimSpace(currentBlock.String()),
					Position:  blockPosition,
					BlockType: blockType,
				})
				currentBlock.Reset()
				blockPosition = len(blocks)
			}
			blockDepth++
			inBlock = true
			blockType = "block"
		}

		currentBlock.WriteString(line)
		currentBlock.WriteString("\n")

		if strings.Contains(trimmedUpper, "END") {
			blockDepth--
			if blockDepth == 0 && inBlock {
				// Complete block found
				blocks = append(blocks, ExcerptBlock{
					Content:   strings.TrimSpace(currentBlock.String()),
					Position:  blockPosition,
					BlockType: "block",
				})
				currentBlock.Reset()
				inBlock = false
				blockType = "statement"
				blockPosition = len(blocks)
			}
		}

		// Detect complete statements (semicolon-terminated, not in block)
		if !inBlock && strings.HasSuffix(trimmed, ";") {
			blocks = append(blocks, ExcerptBlock{
				Content:   strings.TrimSpace(currentBlock.String()),
				Position:  blockPosition,
				BlockType: "statement",
			})
			currentBlock.Reset()
			blockType = "statement"
			blockPosition = len(blocks)
		}

		// Detect header comments (lines starting with --)
		if strings.HasPrefix(trimmed, "--") && !inBlock && currentBlock.Len() <= len(line)+1 {
			if currentBlock.Len() > 0 {
				blocks = append(blocks, ExcerptBlock{
					Content:   strings.TrimSpace(currentBlock.String()),
					Position:  blockPosition,
					BlockType: "comment",
				})
				currentBlock.Reset()
			}
			blockType = "comment"
			blockPosition = len(blocks)
		}
	}

	// Add remaining content
	if currentBlock.Len() > 0 {
		blocks = append(blocks, ExcerptBlock{
			Content:   strings.TrimSpace(currentBlock.String()),
			Position:  blockPosition,
			BlockType: blockType,
		})
	}

	return blocks
}

// scoreSQLBlocks scores SQL blocks based on relevance and structural importance
func (ce *CodeExtractor) scoreSQLBlocks(blocks []ExcerptBlock, query string) []ExcerptBlock {
	queryTerms := extractQueryTerms(query)

	for i := range blocks {
		block := &blocks[i]

		// Base score: structural importance
		switch block.BlockType {
		case "procedure_header":
			block.Score = 100.0 // Highest priority - always include if possible
		case "comment":
			// Check if it's a header comment (parameter definitions, purpose)
			if containsHeaderKeywords(block.Content) {
				block.Score = 90.0
			} else {
				block.Score = 30.0
			}
		case "block":
			block.Score = 60.0 // BEGIN/END blocks are important
		case "statement":
			block.Score = 40.0
		default:
			block.Score = 20.0
		}

		// Boost score based on query term matches
		contentLower := strings.ToLower(block.Content)
		matchCount := 0
		for _, term := range queryTerms {
			if strings.Contains(contentLower, term) {
				matchCount++
			}
		}

		if len(queryTerms) > 0 {
			matchRatio := float64(matchCount) / float64(len(queryTerms))
			block.Score += matchRatio * 30.0
		}

		// Penalize very long blocks slightly (they consume too much space)
		if len(block.Content) > 1000 {
			block.Score *= 0.9
		}
	}

	return blocks
}

// containsHeaderKeywords checks if a comment contains header keywords
func containsHeaderKeywords(content string) bool {
	contentUpper := strings.ToUpper(content)
	keywords := []string{
		"@", "PARAMETER", "PARAM", "RETURNS", "PURPOSE",
		"DESCRIPTION", "AUTHOR", "CREATED", "MODIFIED",
		"ИДЕНТИФИКАТОР", "РАСЧЕТ", // Russian keywords
	}

	for _, keyword := range keywords {
		if strings.Contains(contentUpper, keyword) {
			return true
		}
	}

	return false
}

// extractQueryTerms extracts meaningful terms from query
func extractQueryTerms(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var terms []string

	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"this": true, "that": true, "these": true, "those": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"me": true, "explain": true, "what": true, "how": true, "why": true,
	}

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		if len(word) >= 3 && !stopWords[word] {
			terms = append(terms, word)
		}
	}

	return terms
}

// selectTopBlocks selects top-scored blocks within maxLength limit
func (ce *CodeExtractor) selectTopBlocks(blocks []ExcerptBlock, maxLength int) []ExcerptBlock {
	// Sort by score descending
	sortedBlocks := make([]ExcerptBlock, len(blocks))
	copy(sortedBlocks, blocks)

	// Simple bubble sort (blocks are typically small in number)
	for i := 0; i < len(sortedBlocks)-1; i++ {
		for j := 0; j < len(sortedBlocks)-i-1; j++ {
			if sortedBlocks[j].Score < sortedBlocks[j+1].Score {
				sortedBlocks[j], sortedBlocks[j+1] = sortedBlocks[j+1], sortedBlocks[j]
			}
		}
	}

	var selected []ExcerptBlock
	currentLength := 0

	for _, block := range sortedBlocks {
		blockLen := len(block.Content) + 2 // +2 for newlines
		if currentLength+blockLen > maxLength && currentLength > 0 {
			break
		}
		selected = append(selected, block)
		currentLength += blockLen
	}

	return selected
}

// buildCodeResult builds final result preserving original order
func (ce *CodeExtractor) buildCodeResult(blocks []ExcerptBlock, totalBlocks int) string {
	// Sort selected blocks back to original order
	sortedBlocks := make([]ExcerptBlock, len(blocks))
	copy(sortedBlocks, blocks)

	for i := 0; i < len(sortedBlocks)-1; i++ {
		for j := 0; j < len(sortedBlocks)-i-1; j++ {
			if sortedBlocks[j].Position > sortedBlocks[j+1].Position {
				sortedBlocks[j], sortedBlocks[j+1] = sortedBlocks[j+1], sortedBlocks[j]
			}
		}
	}

	var result strings.Builder
	prevPosition := -1

	for _, block := range sortedBlocks {
		// Add separator if blocks are not consecutive
		if prevPosition >= 0 && block.Position > prevPosition+1 {
			result.WriteString("\n...\n\n")
		}

		result.WriteString(block.Content)
		result.WriteString("\n\n")
		prevPosition = block.Position
	}

	excerpt := strings.TrimSpace(result.String())

	// Add ellipsis if we excluded content
	if len(sortedBlocks) < totalBlocks {
		if sortedBlocks[0].Position > 0 {
			excerpt = "...\n\n" + excerpt
		}
		if sortedBlocks[len(sortedBlocks)-1].Position < totalBlocks-1 {
			excerpt = excerpt + "\n\n..."
		}
	}

	return excerpt
}

// truncateAtSQLBoundary truncates SQL at a safe boundary (statement or block end)
func (ce *CodeExtractor) truncateAtSQLBoundary(content string, maxLength int) string {
	if len(content) <= maxLength {
		return content
	}

	truncated := content[:maxLength]

	// Try to find last semicolon
	lastSemicolon := strings.LastIndex(truncated, ";")
	if lastSemicolon > maxLength/2 {
		return strings.TrimSpace(content[:lastSemicolon+1]) + "\n..."
	}

	// Try to find last complete line
	lastNewline := strings.LastIndex(truncated, "\n")
	if lastNewline > maxLength/2 {
		return strings.TrimSpace(content[:lastNewline]) + "\n..."
	}

	return truncated + "..."
}

// extractGenericCodeExcerpt extracts code for non-SQL languages
func (ce *CodeExtractor) extractGenericCodeExcerpt(content string, query string, maxLength int) string {
	// For now, use simple line-based extraction
	// Future: implement language-specific parsers for Go, Python, etc.

	lines := strings.Split(content, "\n")
	queryTerms := extractQueryTerms(query)

	// Score lines based on query relevance
	type scoredLine struct {
		line     string
		score    float64
		position int
	}

	scored := make([]scoredLine, len(lines))
	for i, line := range lines {
		lineLower := strings.ToLower(line)
		matchCount := 0

		for _, term := range queryTerms {
			if strings.Contains(lineLower, term) {
				matchCount++
			}
		}

		score := 0.0
		if len(queryTerms) > 0 {
			score = float64(matchCount) / float64(len(queryTerms)) * 100.0
		}

		// Boost lines with function/class definitions
		if strings.Contains(lineLower, "func ") ||
		   strings.Contains(lineLower, "def ") ||
		   strings.Contains(lineLower, "class ") {
			score += 50.0
		}

		// Boost comment lines at the top
		if i < 10 && (strings.HasPrefix(strings.TrimSpace(line), "//") ||
		              strings.HasPrefix(strings.TrimSpace(line), "#")) {
			score += 30.0
		}

		scored[i] = scoredLine{line: line, score: score, position: i}
	}

	// Sort by score
	for i := 0; i < len(scored)-1; i++ {
		for j := 0; j < len(scored)-i-1; j++ {
			if scored[j].score < scored[j+1].score {
				scored[j], scored[j+1] = scored[j+1], scored[j]
			}
		}
	}

	// Select lines within maxLength
	var selected []scoredLine
	currentLength := 0

	for _, sl := range scored {
		lineLen := len(sl.line) + 1
		if currentLength+lineLen > maxLength && currentLength > 0 {
			break
		}
		if sl.score > 0 { // Only include lines with some relevance
			selected = append(selected, sl)
			currentLength += lineLen
		}
	}

	// Sort back to original order
	for i := 0; i < len(selected)-1; i++ {
		for j := 0; j < len(selected)-i-1; j++ {
			if selected[j].position > selected[j+1].position {
				selected[j], selected[j+1] = selected[j+1], selected[j]
			}
		}
	}

	var result strings.Builder
	prevPos := -1

	for _, sl := range selected {
		if prevPos >= 0 && sl.position > prevPos+1 {
			result.WriteString("...\n")
		}
		result.WriteString(sl.line)
		result.WriteString("\n")
		prevPos = sl.position
	}

	return strings.TrimSpace(result.String())
}
