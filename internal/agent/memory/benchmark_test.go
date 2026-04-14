package memory

import (
	"fmt"
	"testing"
)

func BenchmarkMinHashSignature_128(b *testing.B) {
	text := "the deployment to production server failed with permission denied error on nginx config when trying to update the SSL certificate for the main domain"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MinHashSignature(text, DefaultNumHashes)
	}
}

func BenchmarkFindCandidatePairs_100(b *testing.B) {
	entries := make([]dedupEntry, 100)
	for i := range entries {
		entries[i] = dedupEntry{
			id:      fmt.Sprintf("entry-%d", i),
			content: fmt.Sprintf("the server deployment number %d failed with error code %d on node %d", i%10, i%5, i%3),
			score:   float64(i),
		}
		entries[i].sig = MinHashSignature(entries[i].content, DefaultNumHashes)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FindCandidatePairs(entries, DedupJaccardThreshold, DefaultLSHBands)
	}
}

func BenchmarkFindCandidatePairs_500(b *testing.B) {
	entries := make([]dedupEntry, 500)
	for i := range entries {
		entries[i] = dedupEntry{
			id:      fmt.Sprintf("entry-%d", i),
			content: fmt.Sprintf("the server deployment number %d failed with error code %d on node %d in region %d", i%20, i%7, i%5, i%3),
			score:   float64(i),
		}
		entries[i].sig = MinHashSignature(entries[i].content, DefaultNumHashes)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FindCandidatePairs(entries, DedupJaccardThreshold, DefaultLSHBands)
	}
}

func BenchmarkExtractEntities_LongText(b *testing.B) {
	text := "Alice talked to Bob about the deployment. Then Sarah Chen called to discuss the API changes. "
	text += "Meanwhile, Dave from engineering was debugging the server. The team at Google had similar issues. "
	text += "Later, Charlie reviewed the pull request that Eve had submitted. Frank fixed the flaky test. "
	for len(text) < 1000 {
		text += "Grace and Heidi worked on the documentation while Ian optimized the database queries. "
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractEntities(text)
	}
}

func BenchmarkJaccardSimilarity(b *testing.B) {
	textA := "the quick brown fox jumps over the lazy dog near the river bank"
	textB := "the quick brown fox jumps over the lazy cat near the river shore"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jaccardSimilarity(textA, textB)
	}
}
