package document

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Parser handles file reading and encoding detection
type Parser struct {
	// SupportedEncodings maps encoding names to their decoders
	SupportedEncodings map[string]encoding.Encoding
}

// NewParser creates a new parser with support for multiple encodings
func NewParser() *Parser {
	return &Parser{
		SupportedEncodings: map[string]encoding.Encoding{
			"UTF-8":          unicode.UTF8,
			"UTF-16LE":       unicode.UTF16(unicode.LittleEndian, unicode.UseBOM),
			"UTF-16BE":       unicode.UTF16(unicode.BigEndian, unicode.UseBOM),
			"Windows-1251":   charmap.Windows1251, // Cyrillic
			"Windows-1252":   charmap.Windows1252, // ANSI/Latin-1
			"ISO-8859-1":     charmap.ISO8859_1,   // Latin-1
		},
	}
}

// ParsedFile represents the result of parsing a file
type ParsedFile struct {
	FilePath     string
	FileName     string
	Content      string
	Encoding     string
	Size         int64
	IsSupported  bool
	Error        error
}

// ParseFile reads a file and detects its encoding, converting to UTF-8
func (p *Parser) ParseFile(filePath string) ParsedFile {
	result := ParsedFile{
		FilePath: filePath,
		FileName: filepath.Base(filePath),
	}

	// Check if file extension is supported
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		// Files without extension (e.g., Dockerfile, Makefile)
		base := strings.ToLower(filepath.Base(filePath))
		if !isKnownNoExtensionFile(base) {
			result.IsSupported = false
			result.Error = fmt.Errorf("unsupported file type: no extension")
			return result
		}
	} else if !IsSupported(ext) {
		result.IsSupported = false
		result.Error = fmt.Errorf("unsupported file extension: %s", ext)
		return result
	}

	result.IsSupported = true

	// Get file size
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		result.Error = fmt.Errorf("failed to stat file: %w", err)
		return result
	}
	result.Size = fileInfo.Size()

	// Read file content
	rawContent, err := os.ReadFile(filePath)
	if err != nil {
		result.Error = fmt.Errorf("failed to read file: %w", err)
		return result
	}

	// Detect encoding and convert to UTF-8
	content, encoding, err := p.detectAndConvert(rawContent)
	if err != nil {
		result.Error = fmt.Errorf("failed to detect encoding: %w", err)
		return result
	}

	result.Content = content
	result.Encoding = encoding

	return result
}

// detectAndConvert attempts to detect the encoding and convert to UTF-8
func (p *Parser) detectAndConvert(data []byte) (string, string, error) {
	// Check for UTF-8 BOM
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return string(data[3:]), "UTF-8-BOM", nil
	}

	// Check for UTF-16 BOM
	if len(data) >= 2 {
		if data[0] == 0xFF && data[1] == 0xFE {
			// UTF-16 LE
			content, err := p.decodeWithEncoding(data, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM))
			if err == nil {
				return content, "UTF-16LE", nil
			}
		}
		if data[0] == 0xFE && data[1] == 0xFF {
			// UTF-16 BE
			content, err := p.decodeWithEncoding(data, unicode.UTF16(unicode.BigEndian, unicode.UseBOM))
			if err == nil {
				return content, "UTF-16BE", nil
			}
		}
	}

	// Try UTF-8 first (most common)
	if isValidUTF8(data) {
		return string(data), "UTF-8", nil
	}

	// Try Windows-1251 (Cyrillic - common in Russian/Eastern European files)
	if content, err := p.decodeWithEncoding(data, charmap.Windows1251); err == nil {
		if looksLikeCyrillic(content) {
			return content, "Windows-1251", nil
		}
	}

	// Try Windows-1252 (ANSI - Western European/Latin)
	if content, err := p.decodeWithEncoding(data, charmap.Windows1252); err == nil {
		return content, "Windows-1252", nil
	}

	// Try ISO-8859-1 (Latin-1)
	if content, err := p.decodeWithEncoding(data, charmap.ISO8859_1); err == nil {
		return content, "ISO-8859-1", nil
	}

	// Fallback: treat as UTF-8 with invalid character replacement
	return string(data), "UTF-8-fallback", nil
}

// decodeWithEncoding attempts to decode data using a specific encoding
func (p *Parser) decodeWithEncoding(data []byte, enc encoding.Encoding) (string, error) {
	decoder := enc.NewDecoder()
	reader := transform.NewReader(bytes.NewReader(data), decoder)
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// isValidUTF8 checks if the data is valid UTF-8
func isValidUTF8(data []byte) bool {
	// Check for common UTF-8 patterns and absence of invalid sequences
	invalidCount := 0
	for i := 0; i < len(data); {
		r, size := decodeRune(data[i:])
		if r == 0xFFFD && size == 1 {
			invalidCount++
			// Allow some invalid characters (< 5%)
			if invalidCount > len(data)/20 {
				return false
			}
		}
		i += size
	}
	return true
}

// decodeRune decodes the first UTF-8 character from data
func decodeRune(data []byte) (rune, int) {
	if len(data) == 0 {
		return 0xFFFD, 0
	}

	b := data[0]

	// ASCII
	if b < 0x80 {
		return rune(b), 1
	}

	// 2-byte sequence
	if b&0xE0 == 0xC0 && len(data) >= 2 {
		r := rune(b&0x1F)<<6 | rune(data[1]&0x3F)
		if r >= 0x80 {
			return r, 2
		}
	}

	// 3-byte sequence
	if b&0xF0 == 0xE0 && len(data) >= 3 {
		r := rune(b&0x0F)<<12 | rune(data[1]&0x3F)<<6 | rune(data[2]&0x3F)
		if r >= 0x800 {
			return r, 3
		}
	}

	// 4-byte sequence
	if b&0xF8 == 0xF0 && len(data) >= 4 {
		r := rune(b&0x07)<<18 | rune(data[1]&0x3F)<<12 | rune(data[2]&0x3F)<<6 | rune(data[3]&0x3F)
		if r >= 0x10000 && r <= 0x10FFFF {
			return r, 4
		}
	}

	return 0xFFFD, 1
}

// looksLikeCyrillic performs heuristic check if text contains Cyrillic characters
func looksLikeCyrillic(text string) bool {
	cyrillicCount := 0
	totalLetters := 0

	for _, r := range text {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= 0x0400 && r <= 0x04FF) {
			totalLetters++
			if r >= 0x0400 && r <= 0x04FF {
				cyrillicCount++
			}
		}
	}

	// If more than 30% of letters are Cyrillic, likely Windows-1251
	if totalLetters > 10 && cyrillicCount > totalLetters/3 {
		return true
	}

	return false
}

// isKnownNoExtensionFile checks if a filename without extension is a known text file
func isKnownNoExtensionFile(basename string) bool {
	knownFiles := map[string]bool{
		"dockerfile":   true,
		"makefile":     true,
		"readme":       true,
		"license":      true,
		"changelog":    true,
		"authors":      true,
		"contributors": true,
		"cmakelists.txt": true,
	}
	return knownFiles[basename]
}
