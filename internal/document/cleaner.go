package document

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"
)

// Cleaner provides text cleaning and normalization functions
type Cleaner struct {
	multipleSpacesRegex  *regexp.Regexp
	multipleNewlinesRegex *regexp.Regexp
	tabsRegex            *regexp.Regexp
}

// NewCleaner creates a new text cleaner
func NewCleaner() *Cleaner {
	return &Cleaner{
		multipleSpacesRegex:   regexp.MustCompile(`[ \t]+`),
		multipleNewlinesRegex: regexp.MustCompile(`\n{3,}`),
		tabsRegex:             regexp.MustCompile(`\t+`),
	}
}

// CleanText performs comprehensive text cleaning to optimize for LLM context
func (c *Cleaner) CleanText(text string) string {
	// Remove zero-width characters and other invisible unicode
	text = c.removeInvisibleCharacters(text)

	// Normalize whitespace
	text = c.normalizeWhitespace(text)

	// Remove excessive blank lines (keep max 2 newlines = 1 blank line)
	text = c.multipleNewlinesRegex.ReplaceAllString(text, "\n\n")

	// Trim leading and trailing whitespace
	text = strings.TrimSpace(text)

	return text
}

// normalizeWhitespace collapses multiple spaces and normalizes tabs
func (c *Cleaner) normalizeWhitespace(text string) string {
	// Convert tabs to single space
	text = c.tabsRegex.ReplaceAllString(text, " ")

	// Collapse multiple spaces to single space
	text = c.multipleSpacesRegex.ReplaceAllString(text, " ")

	// Fix space before newline
	text = strings.ReplaceAll(text, " \n", "\n")

	// Fix space after newline
	text = strings.ReplaceAll(text, "\n ", "\n")

	return text
}

// removeInvisibleCharacters removes zero-width and other invisible unicode characters
func (c *Cleaner) removeInvisibleCharacters(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))

	for _, r := range text {
		// Skip zero-width characters
		switch r {
		case '\u200B', // Zero-width space
			'\u200C', // Zero-width non-joiner
			'\u200D', // Zero-width joiner
			'\uFEFF': // Zero-width no-break space (BOM)
			continue
		}

		// Keep printable characters, spaces, and newlines
		if unicode.IsPrint(r) || r == '\n' || r == '\r' || r == '\t' {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

// RemoveBoilerplate removes common boilerplate text patterns
func (c *Cleaner) RemoveBoilerplate(text string) string {
	// Remove common copyright notices
	copyrightRegex := regexp.MustCompile(`(?i)copyright\s+©?\s*\d{4}.*\n?`)
	text = copyrightRegex.ReplaceAllString(text, "")

	// Remove "confidential" disclaimers
	confidentialRegex := regexp.MustCompile(`(?i)(confidential|proprietary).*\n?`)
	text = confidentialRegex.ReplaceAllString(text, "")

	return text
}

// CalculateHash computes SHA-256 hash of content for deduplication
func (c *Cleaner) CalculateHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// CalculateFileHash computes SHA-256 hash of file content
func CalculateFileHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// IsContentMostlyWhitespace checks if content is mostly whitespace/formatting
func (c *Cleaner) IsContentMostlyWhitespace(text string) bool {
	if len(text) == 0 {
		return true
	}

	nonWhitespaceCount := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			nonWhitespaceCount++
		}
	}

	// If less than 10% is non-whitespace, consider it mostly whitespace
	ratio := float64(nonWhitespaceCount) / float64(len(text))
	return ratio < 0.1
}

// ExtractMeaningfulContent removes lines that are just formatting or separators
func (c *Cleaner) ExtractMeaningfulContent(text string) string {
	lines := strings.Split(text, "\n")
	var meaningfulLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines
		if len(trimmed) == 0 {
			meaningfulLines = append(meaningfulLines, "")
			continue
		}

		// Skip lines that are just separators (===, ---, ___, etc.)
		if c.isSeparatorLine(trimmed) {
			continue
		}

		meaningfulLines = append(meaningfulLines, line)
	}

	return strings.Join(meaningfulLines, "\n")
}

// isSeparatorLine checks if a line is just a separator
func (c *Cleaner) isSeparatorLine(line string) bool {
	if len(line) < 3 {
		return false
	}

	// Check if line is mostly same character repeated
	separatorChars := []rune{'=', '-', '_', '*', '#'}

	for _, char := range separatorChars {
		charCount := strings.Count(line, string(char))
		if float64(charCount)/float64(len(line)) > 0.8 {
			return true
		}
	}

	return false
}
