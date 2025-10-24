package document

// StopWords provides stop word lists for multiple languages
type StopWords struct {
	language string
}

// NewStopWords creates a new stop words provider for a specific language
func NewStopWords(language string) *StopWords {
	return &StopWords{
		language: language,
	}
}

// IsStopWord checks if a word is a stop word in the configured language
func (s *StopWords) IsStopWord(word string) bool {
	switch s.language {
	case "de", "german":
		return s.isGermanStopWord(word)
	case "fr", "french":
		return s.isFrenchStopWord(word)
	case "es", "spanish":
		return s.isSpanishStopWord(word)
	case "ru", "russian":
		return s.isRussianStopWord(word)
	case "en", "english":
		return s.isEnglishStopWord(word)
	default:
		// Default to English
		return s.isEnglishStopWord(word)
	}
}

// isEnglishStopWord checks English stop words
func (s *StopWords) isEnglishStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "be": true, "to": true, "of": true, "and": true,
		"a": true, "in": true, "that": true, "have": true, "i": true,
		"it": true, "for": true, "not": true, "on": true, "with": true,
		"he": true, "as": true, "you": true, "do": true, "at": true,
		"this": true, "but": true, "his": true, "by": true, "from": true,
		"they": true, "we": true, "say": true, "her": true, "she": true,
		"or": true, "an": true, "will": true, "my": true, "one": true,
		"all": true, "would": true, "there": true, "their": true,
		"what": true, "so": true, "up": true, "out": true, "if": true,
		"about": true, "who": true, "get": true, "which": true, "go": true,
		"me": true, "when": true, "make": true, "can": true, "like": true,
		"time": true, "no": true, "just": true, "him": true, "know": true,
		"take": true, "people": true, "into": true, "year": true, "your": true,
		"good": true, "some": true, "could": true, "them": true, "see": true,
		"other": true, "than": true, "then": true, "now": true, "look": true,
		"only": true, "come": true, "its": true, "over": true, "think": true,
		"also": true, "back": true, "after": true, "use": true, "two": true,
		"how": true, "our": true, "work": true, "first": true, "well": true,
		"way": true, "even": true, "new": true, "want": true, "because": true,
		"any": true, "these": true, "give": true, "day": true, "most": true,
		"us": true, "is": true, "was": true, "are": true, "been": true,
		"has": true, "had": true, "were": true, "said": true, "did": true,
	}
	return stopWords[word]
}

// isGermanStopWord checks German stop words
func (s *StopWords) isGermanStopWord(word string) bool {
	stopWords := map[string]bool{
		// Articles
		"der": true, "die": true, "das": true, "den": true, "dem": true,
		"des": true, "ein": true, "eine": true, "einen": true, "einem": true,
		"einer": true, "eines": true,
		// Pronouns
		"ich": true, "du": true, "er": true, "sie": true, "es": true,
		"wir": true, "ihr": true, "mich": true, "mir": true, "dich": true,
		"dir": true, "ihn": true, "ihm": true, "uns": true, "euch": true,
		"ihnen": true, "sich": true,
		// Prepositions
		"in": true, "an": true, "auf": true, "aus": true, "bei": true,
		"mit": true, "nach": true, "von": true, "zu": true, "über": true,
		"unter": true, "für": true, "durch": true, "gegen": true, "ohne": true,
		"um": true, "vor": true, "zwischen": true, "hinter": true,
		// Conjunctions
		"und": true, "oder": true, "aber": true, "denn": true, "sondern": true,
		"als": true, "wenn": true, "weil": true, "dass": true, "ob": true,
		"wie": true, "seit": true, "bis": true, "während": true,
		// Common verbs
		"ist": true, "sind": true, "war": true, "waren": true, "sein": true,
		"haben": true, "hat": true, "hatte": true, "hatten": true,
		"werden": true, "wird": true, "wurde": true, "wurden": true,
		"kann": true, "können": true, "konnte": true, "muss": true,
		"soll": true, "will": true, "würde": true, "möchte": true,
		// Others
		"nicht": true, "nur": true, "noch": true, "auch": true, "schon": true,
		"mehr": true, "sehr": true, "so": true, "dann": true, "doch": true,
		"nun": true, "hier": true, "da": true, "dort": true, "diese": true,
		"dieser": true, "dieses": true, "jetzt": true, "immer": true,
		"alle": true, "alles": true, "man": true, "was": true, "wo": true,
		"wer": true, "welche": true, "welcher": true, "welches": true,
	}
	return stopWords[word]
}

// isFrenchStopWord checks French stop words
func (s *StopWords) isFrenchStopWord(word string) bool {
	stopWords := map[string]bool{
		// Articles
		"le": true, "la": true, "les": true, "un": true, "une": true,
		"des": true, "du": true, "au": true, "aux": true,
		// Pronouns
		"je": true, "tu": true, "il": true, "elle": true, "nous": true,
		"vous": true, "ils": true, "elles": true, "on": true,
		"me": true, "te": true, "se": true, "lui": true, "leur": true,
		"y": true, "en": true, "ce": true, "cela": true, "ça": true,
		"qui": true, "que": true, "quoi": true, "dont": true, "où": true,
		// Prepositions
		"de": true, "à": true, "dans": true, "pour": true, "par": true,
		"sur": true, "avec": true, "sans": true, "sous": true, "vers": true,
		"chez": true, "contre": true, "entre": true, "parmi": true,
		// Conjunctions
		"et": true, "ou": true, "mais": true, "donc": true, "or": true,
		"ni": true, "car": true, "comme": true, "si": true, "quand": true,
		"lorsque": true, "puisque": true, "tandis": true,
		// Common verbs
		"est": true, "sont": true, "était": true, "étaient": true,
		"être": true, "été": true, "avoir": true, "a": true, "ai": true,
		"as": true, "ont": true, "avait": true, "avaient": true,
		"fait": true, "faire": true, "peut": true, "peuvent": true,
		"doit": true, "doivent": true, "va": true, "aller": true,
		// Others
		"ne": true, "pas": true, "plus": true, "non": true, "très": true,
		"tout": true, "tous": true, "toute": true, "toutes": true,
		"même": true, "aussi": true, "encore": true, "déjà": true,
		"bien": true, "mal": true, "moins": true, "alors": true,
		"ainsi": true, "voici": true, "voilà": true,
		"ici": true, "là": true, "cette": true, "ces": true, "cet": true,
		"son": true, "sa": true, "ses": true, "mon": true, "ma": true,
		"mes": true, "ton": true, "ta": true, "tes": true,
		"notre": true, "nos": true, "votre": true, "vos": true,
	}
	return stopWords[word]
}

// isSpanishStopWord checks Spanish stop words
func (s *StopWords) isSpanishStopWord(word string) bool {
	stopWords := map[string]bool{
		// Articles
		"el": true, "la": true, "los": true, "las": true,
		"un": true, "una": true, "unos": true, "unas": true,
		"al": true, "del": true,
		// Pronouns
		"yo": true, "tú": true, "él": true, "ella": true,
		"nosotros": true, "nosotras": true, "vosotros": true, "vosotras": true,
		"ellos": true, "ellas": true, "usted": true, "ustedes": true,
		"me": true, "te": true, "se": true, "le": true, "lo": true,
		"les": true, "nos": true, "os": true,
		"mi": true, "mis": true, "tu": true, "tus": true,
		"su": true, "sus": true, "nuestro": true, "nuestra": true,
		"vuestro": true, "vuestra": true,
		// Prepositions
		"a": true, "ante": true, "bajo": true, "con": true, "contra": true,
		"de": true, "desde": true, "durante": true, "en": true, "entre": true,
		"hacia": true, "hasta": true, "para": true, "por": true, "según": true,
		"sin": true, "sobre": true, "tras": true,
		// Conjunctions
		"y": true, "e": true, "o": true, "u": true, "pero": true,
		"sino": true, "ni": true, "que": true, "si": true, "porque": true,
		"como": true, "cuando": true, "donde": true, "mientras": true,
		"aunque": true, "pues": true,
		// Common verbs
		"es": true, "son": true, "era": true, "eran": true, "ser": true,
		"sido": true, "estar": true, "está": true, "están": true,
		"estaba": true, "estaban": true, "haber": true, "ha": true,
		"han": true, "había": true, "habían": true, "hacer": true,
		"hace": true, "hacen": true, "puede": true, "pueden": true,
		"debe": true, "deben": true, "va": true, "van": true,
		// Others
		"no": true, "sí": true, "más": true, "muy": true, "todo": true,
		"todos": true, "toda": true, "todas": true, "también": true,
		"ya": true, "aún": true, "todavía": true, "solo": true,
		"sólo": true, "bien": true, "mal": true, "tan": true,
		"tanto": true, "poco": true, "mucho": true, "menos": true,
		"así": true, "ahora": true, "entonces": true, "aquí": true,
		"ahí": true, "allí": true, "este": true, "esta": true,
		"estos": true, "estas": true, "ese": true, "esa": true,
		"esos": true, "esas": true, "aquel": true, "aquella": true,
		"esto": true, "eso": true, "aquello": true,
		"cual": true, "cuales": true, "quien": true, "quienes": true,
		"qué": true, "cuál": true, "cuándo": true, "dónde": true,
	}
	return stopWords[word]
}

// isRussianStopWord checks Russian stop words
func (s *StopWords) isRussianStopWord(word string) bool {
	stopWords := map[string]bool{
		// Pronouns
		"я": true, "ты": true, "он": true, "она": true, "оно": true,
		"мы": true, "вы": true, "они": true, "меня": true, "тебя": true,
		"его": true, "её": true, "нас": true, "вас": true, "их": true,
		"мне": true, "тебе": true, "ему": true, "ей": true, "нам": true,
		"вам": true, "им": true, "мной": true, "тобой": true, "ним": true,
		"ней": true, "нами": true, "вами": true, "ними": true,
		"себя": true, "себе": true, "собой": true,
		// Prepositions
		"в": true, "во": true, "на": true, "с": true, "со": true,
		"к": true, "ко": true, "по": true, "о": true, "об": true,
		"от": true, "до": true, "из": true, "у": true, "за": true,
		"над": true, "под": true, "при": true, "через": true, "без": true,
		"для": true, "про": true, "перед": true, "между": true,
		// Conjunctions
		"и": true, "а": true, "но": true, "или": true, "да": true,
		"что": true, "чтобы": true, "если": true, "как": true,
		"когда": true, "потому": true, "поэтому": true, "так": true,
		"также": true, "тоже": true, "либо": true,
		// Common verbs (forms of быть - to be)
		"быть": true, "есть": true, "был": true, "была": true,
		"было": true, "были": true, "будет": true, "будут": true,
		"буду": true, "будем": true, "будешь": true, "будете": true,
		// Particles
		"не": true, "ни": true, "бы": true, "ли": true, "же": true,
		"ведь": true, "уж": true, "вот": true, "вон": true,
		"ну": true, "еще": true, "ещё": true, "уже": true,
		// Others
		"это": true, "этот": true, "эта": true, "эти": true,
		"тот": true, "та": true, "те": true, "весь": true,
		"все": true, "вся": true, "всё": true, "сам": true,
		"сама": true, "само": true, "сами": true, "такой": true,
		"такая": true, "такое": true, "такие": true,
		"который": true, "которая": true, "которое": true, "которые": true,
		"какой": true, "какая": true, "какое": true, "какие": true,
		"кто": true, "где": true, "куда": true, "откуда": true,
		"почему": true, "зачем": true, "сколько": true,
		"здесь": true, "там": true, "тут": true, "туда": true,
		"сюда": true, "теперь": true, "тогда": true, "всегда": true,
		"никогда": true, "можно": true, "нужно": true, "надо": true,
		"очень": true, "более": true, "менее": true, "самый": true,
		"только": true, "даже": true, "лишь": true, "почти": true,
		"вдруг": true, "опять": true, "снова": true,
	}
	return stopWords[word]
}

// DetectLanguage attempts to detect the language of text based on common words
func DetectLanguage(text string) string {
	text = text[:min(len(text), 500)] // Check first 500 chars
	textLower := text

	// Count language-specific indicators
	scores := map[string]int{
		"en": 0,
		"de": 0,
		"fr": 0,
		"es": 0,
		"ru": 0,
	}

	// English indicators
	if contains(textLower, []string{"the ", " and ", " is ", " are ", " have "}) {
		scores["en"] += 10
	}

	// German indicators (check for umlauts and common patterns)
	germanCount := 0
	for _, pattern := range []string{"der ", "die ", "das ", "und ", "ist ", "nicht ", "über ", "für "} {
		if containsPattern(textLower, pattern) {
			germanCount++
		}
	}
	scores["de"] += germanCount * 2

	// Boost for German-specific characters (umlauts)
	for _, r := range text {
		if r == 'ä' || r == 'ö' || r == 'ü' || r == 'Ä' || r == 'Ö' || r == 'Ü' || r == 'ß' {
			scores["de"] += 3
		}
	}

	// French indicators
	if contains(textLower, []string{"le ", "la ", "les ", "de ", "et ", "est ", "à "}) {
		scores["fr"] += 10
	}

	// Spanish indicators
	if contains(textLower, []string{"el ", "la ", "los ", "las ", "de ", "es ", "y "}) {
		scores["es"] += 10
	}

	// Russian indicators (Cyrillic characters)
	cyrillicCount := 0
	for _, r := range text {
		if (r >= 0x0400 && r <= 0x04FF) || (r >= 0x0500 && r <= 0x052F) {
			cyrillicCount++
		}
	}
	if cyrillicCount > 20 {
		scores["ru"] = 100
	}

	// Find language with highest score
	maxScore := 0
	detectedLang := "en" // default to English
	for lang, score := range scores {
		if score > maxScore {
			maxScore = score
			detectedLang = lang
		}
	}

	return detectedLang
}

// contains checks if text contains any of the indicators
func contains(text string, indicators []string) bool {
	count := 0
	for _, indicator := range indicators {
		if containsPattern(text, indicator) {
			count++
		}
	}
	return count >= 3
}

// containsPattern checks if text contains a specific pattern
func containsPattern(text string, pattern string) bool {
	if len(text) == 0 || len(pattern) == 0 {
		return false
	}
	for i := 0; i <= len(text)-len(pattern); i++ {
		if text[i:i+len(pattern)] == pattern {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
