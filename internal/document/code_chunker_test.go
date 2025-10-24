package document

import (
	"strings"
	"testing"
)

func TestDetectLanguageCode(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name: "Go code",
			code: `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}`,
			expected: "go",
		},
		{
			name: "Python code",
			code: `def calculate_total(items):
    total = 0
    for item in items:
        total += item.price
    return total`,
			expected: "python",
		},
		{
			name: "JavaScript code",
			code: `function calculateTotal(items) {
    const total = items.reduce((sum, item) => sum + item.price, 0);
    return total;
}`,
			expected: "javascript",
		},
		{
			name: "Rust code",
			code: `fn calculate_total(items: &[Item]) -> f64 {
    items.iter().map(|item| item.price).sum()
}`,
			expected: "rust",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewCodeChunker("")
			detected := chunker.detectLanguage(tt.code)
			if detected != tt.expected {
				t.Errorf("detectLanguage() = %v, want %v", detected, tt.expected)
			}
		})
	}
}

func TestChunkGoCode(t *testing.T) {
	goCode := `package main

import "fmt"

func add(a, b int) int {
	return a + b
}

func subtract(a, b int) int {
	return a - b
}

type Calculator struct {
	value int
}

func (c *Calculator) Add(x int) {
	c.value += x
}

func main() {
	calc := Calculator{value: 0}
	calc.Add(5)
	fmt.Println(calc.value)
}`

	chunker := NewCodeChunker("go")
	chunks := chunker.ChunkCode(goCode, 2000)

	if len(chunks) == 0 {
		t.Fatal("Expected chunks, got none")
	}

	// Should have separate chunks for functions
	functionCount := 0
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "func ") {
			functionCount++
		}
	}

	if functionCount < 3 {
		t.Errorf("Expected at least 3 function chunks, got %d", functionCount)
	}
}

func TestChunkPythonCode(t *testing.T) {
	pythonCode := `def calculate_sum(numbers):
    total = 0
    for num in numbers:
        total += num
    return total

class Calculator:
    def __init__(self):
        self.value = 0

    def add(self, x):
        self.value += x

    def get_value(self):
        return self.value

def main():
    calc = Calculator()
    calc.add(5)
    print(calc.get_value())`

	chunker := NewCodeChunker("python")
	chunks := chunker.ChunkCode(pythonCode, 2000)

	if len(chunks) == 0 {
		t.Fatal("Expected chunks, got none")
	}

	// Should have chunks for functions and class
	hasClass := false
	hasFunctions := false

	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "class Calculator") {
			hasClass = true
		}
		if strings.Contains(chunk.Content, "def calculate_sum") {
			hasFunctions = true
		}
	}

	if !hasClass {
		t.Error("Expected class chunk")
	}
	if !hasFunctions {
		t.Error("Expected function chunks")
	}
}

func TestIsCodeFile(t *testing.T) {
	tests := []struct {
		filename string
		expected bool
	}{
		{"main.go", true},
		{"script.py", true},
		{"app.js", true},
		{"component.tsx", true},
		{"Main.java", true},
		{"lib.rs", true},
		{"readme.md", false},
		{"data.json", false},
		{"config.yaml", false},
		{"document.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := IsCodeFile(tt.filename)
			if result != tt.expected {
				t.Errorf("IsCodeFile(%s) = %v, want %v", tt.filename, result, tt.expected)
			}
		})
	}
}

func TestCodeChunkOptimization(t *testing.T) {
	code := `func example() {
    // This is a comment
    x := 1



    y := 2
    return x + y
}`

	chunker := NewCodeChunker("go")
	block := CodeBlock{
		Type:    "function",
		Name:    "example",
		Content: code,
	}

	optimized := chunker.optimizeCodeBlock(block)

	// Should remove excessive blank lines but keep structure
	blankLineCount := strings.Count(optimized, "\n\n\n")
	if blankLineCount > 0 {
		t.Error("Should remove excessive blank lines")
	}

	// Should keep comments (they're valuable in code)
	if !strings.Contains(optimized, "// This is a comment") {
		t.Error("Should preserve comments")
	}

	// Should remove trailing whitespace
	lines := strings.Split(optimized, "\n")
	for i, line := range lines {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Errorf("Line %d has trailing whitespace: %q", i, line)
		}
	}
}

func TestChunkLargeCodeBlock(t *testing.T) {
	// Create a large function
	largeFunc := `func processData(data []string) {
    results := make([]string, 0)
    for _, item := range data {
        if len(item) > 10 {
            results = append(results, item)
        }
    }
    // ... 100 more lines ...
    ` + strings.Repeat("    results = append(results, \"test\")\n", 100) + `
    return results
}`

	chunker := NewCodeChunker("go")
	block := CodeBlock{
		Type:      "function",
		Name:      "processData",
		Content:   largeFunc,
		StartLine: 0,
		EndLine:   103,
	}

	// Split into smaller chunks (max 500 chars)
	subChunks := chunker.splitLargeBlock(block, 500)

	if len(subChunks) <= 1 {
		t.Errorf("Expected multiple chunks for large block, got %d", len(subChunks))
	}

	// Each chunk should be under max size (with small tolerance)
	for i, chunk := range subChunks {
		if len(chunk) > 550 { // Small tolerance
			t.Errorf("Chunk %d exceeds max size: %d chars", i, len(chunk))
		}
	}
}

func TestCodeDetectionInChunker(t *testing.T) {
	regularChunker := NewChunker()

	codeContent := `package main

func main() {
	fmt.Println("Hello")
}`

	textContent := `This is a regular document.
It contains some text about programming.
But it is not actual code.`

	// Test code detection
	if !regularChunker.isCodeContent(codeContent) {
		t.Error("Should detect code content")
	}

	if regularChunker.isCodeContent(textContent) {
		t.Error("Should not detect text as code")
	}
}

func TestCodeChunkingIntegration(t *testing.T) {
	goCode := `package calculator

// Add adds two numbers
func Add(a, b int) int {
	return a + b
}

// Subtract subtracts b from a
func Subtract(a, b int) int {
	return a - b
}`

	regularChunker := NewChunker()
	chunks := regularChunker.ChunkDocument(goCode)

	// Should chunk by functions, not arbitrary size
	if len(chunks) < 2 {
		t.Errorf("Expected at least 2 chunks (one per function), got %d", len(chunks))
	}

	// Each chunk should contain a complete function
	for i, chunk := range chunks {
		if !strings.Contains(chunk.Content, "func ") {
			t.Errorf("Chunk %d doesn't contain 'func'", i)
		}
	}
}
