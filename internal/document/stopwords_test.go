package document

import "testing"

func TestLanguageDetection(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "English text",
			text:     "The quick brown fox jumps over the lazy dog. This is a test of the language detection system.",
			expected: "en",
		},
		{
			name:     "German text",
			text:     "Der schnelle braune Fuchs springt über den faulen Hund. Das ist ein Test des Spracherkennungssystems.",
			expected: "de",
		},
		{
			name:     "French text",
			text:     "Le renard brun rapide saute par-dessus le chien paresseux. Ceci est un test du système de détection de langue.",
			expected: "fr",
		},
		{
			name:     "Spanish text",
			text:     "El rápido zorro marrón salta sobre el perro perezoso. Esta es una prueba del sistema de detección de idioma.",
			expected: "es",
		},
		{
			name:     "Russian text",
			text:     "Быстрая коричневая лиса прыгает через ленивую собаку. Это тест системы определения языка.",
			expected: "ru",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detected := DetectLanguage(tt.text)
			if detected != tt.expected {
				t.Errorf("DetectLanguage() = %v, want %v", detected, tt.expected)
			}
		})
	}
}

func TestStopWordsEnglish(t *testing.T) {
	sw := NewStopWords("en")

	stopWords := []string{"the", "and", "is", "are", "was"}
	for _, word := range stopWords {
		if !sw.IsStopWord(word) {
			t.Errorf("Expected '%s' to be a stop word in English", word)
		}
	}

	contentWords := []string{"database", "schema", "query", "optimization"}
	for _, word := range contentWords {
		if sw.IsStopWord(word) {
			t.Errorf("Expected '%s' NOT to be a stop word in English", word)
		}
	}
}

func TestStopWordsGerman(t *testing.T) {
	sw := NewStopWords("de")

	stopWords := []string{"der", "die", "das", "und", "ist"}
	for _, word := range stopWords {
		if !sw.IsStopWord(word) {
			t.Errorf("Expected '%s' to be a stop word in German", word)
		}
	}

	contentWords := []string{"datenbank", "schema", "abfrage"}
	for _, word := range contentWords {
		if sw.IsStopWord(word) {
			t.Errorf("Expected '%s' NOT to be a stop word in German", word)
		}
	}
}

func TestStopWordsFrench(t *testing.T) {
	sw := NewStopWords("fr")

	stopWords := []string{"le", "la", "les", "de", "et", "est"}
	for _, word := range stopWords {
		if !sw.IsStopWord(word) {
			t.Errorf("Expected '%s' to be a stop word in French", word)
		}
	}

	contentWords := []string{"base", "données", "requête"}
	for _, word := range contentWords {
		if sw.IsStopWord(word) {
			t.Errorf("Expected '%s' NOT to be a stop word in French", word)
		}
	}
}

func TestStopWordsSpanish(t *testing.T) {
	sw := NewStopWords("es")

	stopWords := []string{"el", "la", "los", "de", "y", "es"}
	for _, word := range stopWords {
		if !sw.IsStopWord(word) {
			t.Errorf("Expected '%s' to be a stop word in Spanish", word)
		}
	}

	contentWords := []string{"base", "datos", "consulta"}
	for _, word := range contentWords {
		if sw.IsStopWord(word) {
			t.Errorf("Expected '%s' NOT to be a stop word in Spanish", word)
		}
	}
}

func TestStopWordsRussian(t *testing.T) {
	sw := NewStopWords("ru")

	stopWords := []string{"и", "в", "не", "что", "на"}
	for _, word := range stopWords {
		if !sw.IsStopWord(word) {
			t.Errorf("Expected '%s' to be a stop word in Russian", word)
		}
	}

	contentWords := []string{"база", "данных", "запрос"}
	for _, word := range contentWords {
		if sw.IsStopWord(word) {
			t.Errorf("Expected '%s' NOT to be a stop word in Russian", word)
		}
	}
}

func TestExtractorWithLanguage(t *testing.T) {
	tests := []struct {
		name     string
		language string
		query    string
		content  string
	}{
		{
			name:     "English extraction",
			language: "en",
			query:    "database schema",
			content:  "The database schema is defined in the configuration. The schema contains all table definitions.",
		},
		{
			name:     "German extraction",
			language: "de",
			query:    "datenbank schema",
			content:  "Das Datenbank-Schema ist in der Konfiguration definiert. Das Schema enthält alle Tabellendefinitionen.",
		},
		{
			name:     "French extraction",
			language: "fr",
			query:    "base données",
			content:  "Le schéma de base de données est défini dans la configuration. Le schéma contient toutes les définitions de tables.",
		},
		{
			name:     "Spanish extraction",
			language: "es",
			query:    "base datos",
			content:  "El esquema de base de datos está definido en la configuración. El esquema contiene todas las definiciones de tablas.",
		},
		{
			name:     "Russian extraction",
			language: "ru",
			query:    "база данных",
			content:  "Схема базы данных определена в конфигурации. Схема содержит все определения таблиц.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := NewExtractorWithLanguage(tt.language)
			excerpt := extractor.ExtractRelevantExcerpt(tt.content, tt.query, 500)

			if len(excerpt) == 0 {
				t.Errorf("ExtractRelevantExcerpt() returned empty excerpt")
			}

			// Verify language is set correctly
			if extractor.GetLanguage() != tt.language {
				t.Errorf("Expected language %s, got %s", tt.language, extractor.GetLanguage())
			}
		})
	}
}
