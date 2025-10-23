package document

// SupportedExtensions defines file extensions that can be parsed as text documents
var SupportedExtensions = map[string]bool{
	// Plain text
	".txt":  true,
	".log":  true,
	".md":   true,
	".rtf":  false, // Rich text requires special parsing

	// Data formats
	".json": true,
	".xml":  true,
	".yaml": true,
	".yml":  true,
	".csv":  true,
	".tsv":  true,

	// Configuration
	".conf": true,
	".cfg":  true,
	".ini":  true,
	".env":  true,
	".properties": true,

	// Programming languages
	".go":   true,
	".java": true,
	".py":   true,
	".js":   true,
	".ts":   true,
	".jsx":  true,
	".tsx":  true,
	".c":    true,
	".cpp":  true,
	".cc":   true,
	".cxx":  true,
	".h":    true,
	".hpp":  true,
	".rs":   true,
	".rb":   true,
	".php":  true,
	".cs":   true,
	".swift": true,
	".kt":   true,
	".scala": true,
	".r":    true,
	".m":    true,
	".mm":   true,

	// Web
	".html": true,
	".htm":  true,
	".css":  true,
	".scss": true,
	".sass": true,
	".less": true,

	// Scripting
	".sh":   true,
	".bash": true,
	".zsh":  true,
	".fish": true,
	".bat":  true,
	".cmd":  true,
	".ps1":  true,

	// SQL
	".sql": true,

	// Markup
	".tex":  true,
	".rst":  true,
	".adoc": true,

	// Other
	".gitignore": true,
	".dockerignore": true,
}

// IsSupported checks if a file extension is supported for text parsing
func IsSupported(ext string) bool {
	supported, exists := SupportedExtensions[ext]
	return exists && supported
}
