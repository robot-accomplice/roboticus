package agent

import "strings"

type TopicCategory string

const (
	TopicGeneral   TopicCategory = "general"
	TopicTechnical TopicCategory = "technical"
	TopicCreative  TopicCategory = "creative"
	TopicFinancial TopicCategory = "financial"
	TopicSupport   TopicCategory = "support"
	TopicResearch  TopicCategory = "research"
)

type TopicResult struct {
	Primary    TopicCategory
	Secondary  TopicCategory
	Confidence float64
	Keywords   []string
}

func DetectTopic(messages []string) TopicResult {
	if len(messages) == 0 {
		return TopicResult{Primary: TopicGeneral, Confidence: 0.0}
	}
	combined := strings.ToLower(strings.Join(messages, " "))
	scores := map[TopicCategory]int{
		TopicTechnical: countKeywords(combined, []string{"code", "bug", "api", "function", "error", "deploy", "server", "database", "git", "test"}),
		TopicCreative:  countKeywords(combined, []string{"write", "poem", "story", "creative", "design", "art", "music", "compose"}),
		TopicFinancial: countKeywords(combined, []string{"wallet", "transfer", "balance", "payment", "cost", "price", "budget", "revenue"}),
		TopicSupport:   countKeywords(combined, []string{"help", "issue", "problem", "broken", "fix", "support", "how to", "stuck"}),
		TopicResearch:  countKeywords(combined, []string{"research", "analyze", "compare", "study", "data", "report", "survey", "findings"}),
	}

	primary := TopicGeneral
	secondary := TopicGeneral
	bestScore := 0
	secondScore := 0
	for cat, score := range scores {
		if score > bestScore {
			secondary = primary
			secondScore = bestScore
			primary = cat
			bestScore = score
		} else if score > secondScore {
			secondary = cat
			secondScore = score
		}
	}

	confidence := 0.0
	if bestScore > 0 {
		totalWords := len(strings.Fields(combined))
		if totalWords > 0 {
			confidence = float64(bestScore) / float64(totalWords)
			if confidence > 1.0 {
				confidence = 1.0
			}
		}
	}

	var keywords []string
	for _, word := range strings.Fields(combined) {
		if len(keywords) >= 5 {
			break
		}
		if len(word) > 4 {
			keywords = append(keywords, word)
		}
	}

	return TopicResult{Primary: primary, Secondary: secondary, Confidence: confidence, Keywords: keywords}
}

func countKeywords(text string, keywords []string) int {
	count := 0
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			count++
		}
	}
	return count
}
