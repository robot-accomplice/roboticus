// manager_entities.go — entity extraction for relationship memory ingestion.
// Extracted from manager.go to keep file under 500-line cap.

package memory

import "strings"

// entityExclusions are words that commonly start sentences or are not proper nouns.
var entityExclusions = map[string]bool{
	"the": true, "this": true, "that": true, "these": true, "those": true,
	"it": true, "its": true, "we": true, "they": true, "he": true, "she": true,
	"however": true, "therefore": true, "furthermore": true, "meanwhile": true,
	"also": true, "but": true, "and": true, "or": true, "so": true, "yet": true,
	"if": true, "when": true, "where": true, "while": true, "because": true,
	"after": true, "before": true, "since": true, "until": true,
	"january": true, "february": true, "march": true, "april": true,
	"may": true, "june": true, "july": true, "august": true,
	"september": true, "october": true, "november": true, "december": true,
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
	"api": true, "url": true, "http": true, "https": true, "sql": true,
	"css": true, "html": true, "json": true, "xml": true, "cli": true,
	"yes": true, "no": true, "sure": true, "ok": true, "here": true,
	"there": true, "what": true, "how": true, "why": true, "who": true,
	"i": true, "my": true, "me": true, "your": true, "you": true,
}

// commonNonNameWords are words that frequently appear capitalized at sentence start
// but are almost never proper nouns.
var commonNonNameWords = map[string]bool{
	"let": true, "make": true, "run": true, "try": true, "check": true,
	"set": true, "get": true, "use": true, "add": true, "fix": true,
	"see": true, "look": true, "just": true, "can": true, "should": true,
	"will": true, "would": true, "could": true, "do": true, "did": true,
	"have": true, "had": true, "has": true, "was": true, "were": true,
	"been": true, "being": true, "is": true, "are": true, "am": true,
	"then": true, "next": true, "now": true, "first": true, "last": true,
	"once": true, "still": true, "even": true, "each": true, "every": true,
	"both": true, "either": true, "neither": true, "some": true, "any": true,
	"all": true, "most": true, "many": true, "much": true, "more": true,
	"less": true, "few": true, "other": true, "another": true, "such": true,
	"in": true, "on": true, "at": true, "to": true, "for": true,
	"with": true, "from": true, "by": true, "about": true, "into": true,
	"our": true, "their": true, "his": true, "her": true,
	"one": true, "two": true, "three": true, "four": true, "five": true,
	"note": true, "error": true, "warning": true, "update": true,
	"found": true, "created": true, "deleted": true, "changed": true,
	"started": true, "stopped": true, "running": true, "loading": true,
	"tested": true, "deployed": true, "built": true, "installed": true,
	"hello": true, "hi": true, "hey": true, "thanks": true, "thank": true,
	"please": true, "sorry": true, "great": true, "good": true,
	"welcome": true, "bye": true, "goodbye": true,
}

const maxEntitiesPerMessage = 5

// extractEntities finds potential entity references in text.
func extractEntities(text string) []string {
	var entities []string
	seen := make(map[string]bool)

	words := strings.Fields(text)
	for _, w := range words {
		if strings.HasPrefix(w, "@") && len(w) > 1 {
			name := strings.Trim(w[1:], ".,!?;:")
			if name != "" && !seen[strings.ToLower(name)] {
				entities = append(entities, name)
				seen[strings.ToLower(name)] = true
			}
		}
	}

	afterSentenceEnd := true
	var currentName []string

	for _, w := range words {
		clean := strings.Trim(w, ".,!?;:\"'()[]")
		endsSentence := strings.HasSuffix(w, ".") || strings.HasSuffix(w, "!") || strings.HasSuffix(w, "?")
		isCapitalized := len(clean) >= 2 && isUpperRune(rune(clean[0])) && !isAllUpper(clean)
		isLikelyName := isCapitalized && !entityExclusions[strings.ToLower(clean)]
		if afterSentenceEnd && isLikelyName {
			isLikelyName = !commonNonNameWords[strings.ToLower(clean)]
		}
		if isLikelyName {
			currentName = append(currentName, clean)
		} else {
			if len(currentName) >= 1 {
				name := strings.Join(currentName, " ")
				lower := strings.ToLower(name)
				if !seen[lower] && !entityExclusions[lower] {
					entities = append(entities, name)
					seen[lower] = true
				}
			}
			currentName = nil
		}
		afterSentenceEnd = endsSentence
		if len(entities) >= maxEntitiesPerMessage {
			break
		}
	}

	if len(currentName) >= 1 && len(entities) < maxEntitiesPerMessage {
		name := strings.Join(currentName, " ")
		lower := strings.ToLower(name)
		if !seen[lower] && !entityExclusions[lower] {
			entities = append(entities, name)
		}
	}

	wordFreq := make(map[string]int)
	for _, w := range words {
		clean := strings.Trim(w, ".,!?;:\"'()[]")
		if len(clean) >= 2 && isUpperRune(rune(clean[0])) && !isAllUpper(clean) {
			wordFreq[strings.ToLower(clean)]++
		}
	}
	for word, count := range wordFreq {
		if count >= 2 && !entityExclusions[word] && !seen[word] && len(entities) < maxEntitiesPerMessage {
			titled := strings.ToUpper(word[:1]) + word[1:]
			entities = append(entities, titled)
			seen[word] = true
		}
	}

	return entities
}

func isUpperRune(r rune) bool { return r >= 'A' && r <= 'Z' }
func isAllUpper(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}
