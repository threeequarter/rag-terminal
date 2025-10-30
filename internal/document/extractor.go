package document

import (
	"sort"
	"strings"
)

// Extractor extracts relevant excerpts from chunks based on query
type Extractor struct {
	cleaner   *Cleaner
	stopWords *StopWords
	language  string
}

// NewExtractor creates a new excerpt extractor with auto-detected language
func NewExtractor() *Extractor {
	return &Extractor{
		cleaner:   NewCleaner(),
		stopWords: NewStopWords("en"), // default to English
		language:  "en",
	}
}

// NewExtractorWithLanguage creates a new excerpt extractor for a specific language
func NewExtractorWithLanguage(language string) *Extractor {
	return &Extractor{
		cleaner:   NewCleaner(),
		stopWords: NewStopWords(language),
		language:  language,
	}
}

// ExtractRelevantExcerpt extracts the most relevant portion of a chunk for a query
func (e *Extractor) ExtractRelevantExcerpt(chunkContent string, query string, maxLength int) string {
	return e.ExtractRelevantExcerptWithPath(chunkContent, query, maxLength, "")
}

// ExtractRelevantExcerptWithPath extracts relevant excerpt with file path awareness
// If filePath indicates a code file, uses code-aware extraction
func (e *Extractor) ExtractRelevantExcerptWithPath(chunkContent string, query string, maxLength int, filePath string) string {
	// Check if this is a code file and use code-aware extraction
	if filePath != "" && IsCodeFile(filePath) {
		codeExtractor := NewCodeExtractor(filePath)
		return codeExtractor.ExtractCodeExcerpt(chunkContent, query, maxLength)
	}

	// Fall back to original text-based extraction
	return e.extractTextExcerpt(chunkContent, query, maxLength)
}

// extractTextExcerpt performs the original text-based extraction
func (e *Extractor) extractTextExcerpt(chunkContent string, query string, maxLength int) string {
	// Auto-detect language from content if not explicitly set
	if e.language == "en" && len(chunkContent) > 100 {
		detectedLang := DetectLanguage(chunkContent)
		if detectedLang != "en" {
			e.language = detectedLang
			e.stopWords = NewStopWords(detectedLang)
		}
	}

	// If chunk is already small enough, return as-is
	if len(chunkContent) <= maxLength {
		return chunkContent
	}

	// Split into sentences
	sentences := e.splitIntoSentences(chunkContent)
	if len(sentences) == 0 {
		// Fallback: just truncate
		return e.truncateAtBoundary(chunkContent, maxLength)
	}

	// Score each sentence based on query relevance
	scoredSentences := e.scoreSentences(sentences, query)

	// Sort by score descending
	sort.Slice(scoredSentences, func(i, j int) bool {
		return scoredSentences[i].score > scoredSentences[j].score
	})

	// Take top sentences until we hit max length
	var selectedSentences []scoredSentence
	currentLength := 0

	for _, ss := range scoredSentences {
		sentenceLen := len(ss.sentence) + 1 // +1 for space
		if currentLength+sentenceLen > maxLength && currentLength > 0 {
			break
		}
		selectedSentences = append(selectedSentences, ss)
		currentLength += sentenceLen
	}

	// Sort selected sentences back to original order
	sort.Slice(selectedSentences, func(i, j int) bool {
		return selectedSentences[i].position < selectedSentences[j].position
	})

	// Join sentences
	var result strings.Builder
	for i, ss := range selectedSentences {
		if i > 0 {
			result.WriteString(" ")
		}
		result.WriteString(ss.sentence)
	}

	excerpt := result.String()

	// If we excluded content, add ellipsis markers
	if len(selectedSentences) < len(sentences) {
		excerpt = "..." + excerpt + "..."
	}

	return excerpt
}

// splitIntoSentences splits text into sentences
func (e *Extractor) splitIntoSentences(text string) []string {
	// Simple sentence splitter - splits on . ! ? followed by space or newline
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		// Check for sentence ending
		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
			// Look ahead to see if followed by space/newline
			if i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\n') {
				sentence := strings.TrimSpace(current.String())
				if len(sentence) > 0 {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	// Add remaining content as last sentence
	if current.Len() > 0 {
		sentence := strings.TrimSpace(current.String())
		if len(sentence) > 0 {
			sentences = append(sentences, sentence)
		}
	}

	return sentences
}

type scoredSentence struct {
	sentence string
	score    float64
	position int
}

// scoreSentences scores sentences based on query relevance
func (e *Extractor) scoreSentences(sentences []string, query string) []scoredSentence {
	queryTerms := e.extractTerms(strings.ToLower(query))

	scored := make([]scoredSentence, len(sentences))
	for i, sentence := range sentences {
		sentenceTerms := e.extractTerms(strings.ToLower(sentence))
		score := e.calculateTermOverlap(queryTerms, sentenceTerms)

		// Boost score for longer sentences (more context)
		lengthBoost := float64(len(sentence)) / 200.0
		if lengthBoost > 2.0 {
			lengthBoost = 2.0
		}

		scored[i] = scoredSentence{
			sentence: sentence,
			score:    score * (1.0 + lengthBoost*0.1),
			position: i,
		}
	}

	return scored
}

// extractTerms extracts significant terms from text
func (e *Extractor) extractTerms(text string) map[string]int {
	terms := make(map[string]int)

	// Split on whitespace and punctuation
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})

	for _, word := range words {
		word = strings.TrimSpace(word)
		if len(word) < 3 {
			// Skip very short words
			continue
		}

		// Skip common stop words using language-specific list
		if e.stopWords.IsStopWord(word) {
			continue
		}

		terms[word]++
	}

	return terms
}

// calculateTermOverlap calculates overlap between query and sentence terms
func (e *Extractor) calculateTermOverlap(queryTerms, sentenceTerms map[string]int) float64 {
	if len(queryTerms) == 0 {
		return 0.0
	}

	overlap := 0
	for term := range queryTerms {
		if _, exists := sentenceTerms[term]; exists {
			overlap++
		}
	}

	return float64(overlap) / float64(len(queryTerms))
}

// GetLanguage returns the currently detected/configured language
func (e *Extractor) GetLanguage() string {
	return e.language
}

// SetLanguage explicitly sets the language for extraction
func (e *Extractor) SetLanguage(language string) {
	e.language = language
	e.stopWords = NewStopWords(language)
}

// truncateAtBoundary truncates text at a word boundary
func (e *Extractor) truncateAtBoundary(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}

	// Find last space before maxLength
	truncated := text[:maxLength]
	lastSpace := strings.LastIndex(truncated, " ")

	if lastSpace > 0 {
		return truncated[:lastSpace] + "..."
	}

	return truncated + "..."
}
