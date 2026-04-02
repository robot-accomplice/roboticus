package agent

import "strings"

// ConsolidationEntry is a memory candidate for deduplication with a relevance score.
type ConsolidationEntry struct {
	ID       string
	Content  string
	Category string
	Score    float64
}

// Consolidator merges similar memory entries using Jaccard similarity.
type Consolidator struct {
	similarityThreshold float64
}

// NewConsolidator creates a Consolidator with the given similarity threshold (0–1).
func NewConsolidator(threshold float64) *Consolidator {
	return &Consolidator{similarityThreshold: threshold}
}

// Consolidate deduplicates entries by merging groups that are similar within
// the same category. The highest-scored entry in each group wins.
func (c *Consolidator) Consolidate(entries []ConsolidationEntry) []ConsolidationEntry {
	if len(entries) <= 1 {
		return entries
	}
	var merged []ConsolidationEntry
	used := make(map[int]bool)

	for i := range entries {
		if used[i] {
			continue
		}
		group := []ConsolidationEntry{entries[i]}
		for j := i + 1; j < len(entries); j++ {
			if used[j] {
				continue
			}
			if c.areSimilar(entries[i], entries[j]) {
				group = append(group, entries[j])
				used[j] = true
			}
		}
		merged = append(merged, c.mergeGroup(group))
		used[i] = true
	}
	return merged
}

func (c *Consolidator) areSimilar(a, b ConsolidationEntry) bool {
	if a.Category != b.Category {
		return false
	}
	return jaccardSimilarity(a.Content, b.Content) >= c.similarityThreshold
}

func (c *Consolidator) mergeGroup(group []ConsolidationEntry) ConsolidationEntry {
	if len(group) == 1 {
		return group[0]
	}
	best := group[0]
	for _, e := range group[1:] {
		if e.Score > best.Score {
			best.Content = e.Content
			best.Score = e.Score
		}
	}
	return best
}

// jaccardSimilarity computes word-level Jaccard similarity between two strings.
func jaccardSimilarity(a, b string) float64 {
	wordsA := strings.Fields(strings.ToLower(a))
	wordsB := strings.Fields(strings.ToLower(b))
	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}
	setA := make(map[string]bool)
	for _, w := range wordsA {
		setA[w] = true
	}
	setB := make(map[string]bool)
	for _, w := range wordsB {
		setB[w] = true
	}
	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
