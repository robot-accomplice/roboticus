package memory

import (
	"math"
	"testing"
)

func TestMinHash_IdenticalTexts(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog"
	sigA := MinHashSignature(text, DefaultNumHashes)
	sigB := MinHashSignature(text, DefaultNumHashes)

	if len(sigA) != DefaultNumHashes {
		t.Fatalf("expected %d hashes, got %d", DefaultNumHashes, len(sigA))
	}

	for i := range sigA {
		if sigA[i] != sigB[i] {
			t.Errorf("hash %d differs: %d != %d", i, sigA[i], sigB[i])
		}
	}

	est := EstimateJaccard(sigA, sigB)
	if est != 1.0 {
		t.Errorf("identical texts should have Jaccard=1.0, got %.4f", est)
	}
}

func TestMinHash_SimilarTexts(t *testing.T) {
	textA := "the deployment to production server failed with permission denied error on nginx config"
	textB := "the deployment to production server failed with permission denied error on apache config"

	sigA := MinHashSignature(textA, DefaultNumHashes)
	sigB := MinHashSignature(textB, DefaultNumHashes)

	// These texts share most bigrams, differing only in "nginx config" vs "apache config".
	exactJaccard := jaccardSimilarity(textA, textB)
	estJaccard := EstimateJaccard(sigA, sigB)

	// MinHash estimate should be within ±0.15 of exact.
	delta := math.Abs(estJaccard - exactJaccard)
	if delta > 0.15 {
		t.Errorf("MinHash estimate %.4f too far from exact %.4f (delta %.4f)", estJaccard, exactJaccard, delta)
	}
}

func TestMinHash_DissimilarTexts(t *testing.T) {
	textA := "the deployment to production server failed with permission denied"
	textB := "I had a wonderful breakfast at the cafe this morning with my friend"

	sigA := MinHashSignature(textA, DefaultNumHashes)
	sigB := MinHashSignature(textB, DefaultNumHashes)

	est := EstimateJaccard(sigA, sigB)
	if est > 0.3 {
		t.Errorf("dissimilar texts should have low Jaccard, got %.4f", est)
	}
}

func TestMinHash_EmptyText(t *testing.T) {
	sig := MinHashSignature("", DefaultNumHashes)
	if len(sig) != DefaultNumHashes {
		t.Fatalf("expected %d hashes even for empty text, got %d", DefaultNumHashes, len(sig))
	}
}

func TestLSH_FindsCandidates(t *testing.T) {
	// Create entries where [0] and [1] are near-duplicates, [2] is different.
	entries := []dedupEntry{
		{id: "a", content: "the server deployment failed because of permission denied on nginx config file", score: 5},
		{id: "b", content: "the server deployment failed because of permission denied on nginx config directory", score: 5},
		{id: "c", content: "I had a wonderful breakfast at the cafe this morning with my friend alice", score: 5},
	}

	for i := range entries {
		entries[i].sig = MinHashSignature(entries[i].content, DefaultNumHashes)
	}

	// Verify exact Jaccard: a-b should be high, a-c should be low.
	abJaccard := jaccardSimilarity(entries[0].content, entries[1].content)
	if abJaccard < 0.7 {
		t.Fatalf("entries 0-1 should be similar (Jaccard=%.4f)", abJaccard)
	}

	pairs := FindCandidatePairs(entries, 0.7, DefaultLSHBands)

	// The pair (0,1) should be found.
	found01 := false
	for _, p := range pairs {
		if (p.i == 0 && p.j == 1) || (p.i == 1 && p.j == 0) {
			found01 = true
		}
	}
	if !found01 {
		t.Error("LSH should have found candidate pair (0,1)")
	}
}

func TestLSH_NoCandidatesForDissimilar(t *testing.T) {
	entries := []dedupEntry{
		{id: "a", content: "the quick brown fox jumps over the lazy dog near the river bank", score: 5},
		{id: "b", content: "machine learning algorithms process large datasets for pattern recognition in real time", score: 5},
	}

	for i := range entries {
		entries[i].sig = MinHashSignature(entries[i].content, DefaultNumHashes)
	}

	pairs := FindCandidatePairs(entries, DedupJaccardThreshold, DefaultLSHBands)
	if len(pairs) != 0 {
		t.Errorf("dissimilar entries should have no candidate pairs, got %d", len(pairs))
	}
}

func TestDedupBatchCap(t *testing.T) {
	// DedupBatchCap should be 500.
	if DedupBatchCap != 500 {
		t.Errorf("DedupBatchCap should be 500, got %d", DedupBatchCap)
	}
}

func TestFindCandidatePairs_NearMissRecovery(t *testing.T) {
	// Create two entries that are very similar (Jaccard ~0.86) but might
	// not share an LSH bucket due to the probabilistic nature of LSH.
	// The second-pass near-miss sweep should catch them.
	entries := []dedupEntry{
		{id: "a", content: "the production server deployment failed because of permission denied error on the nginx configuration file path", score: 5},
		{id: "b", content: "the production server deployment failed because of permission denied error on the apache configuration file path", score: 5},
	}

	exact := jaccardSimilarity(entries[0].content, entries[1].content)
	t.Logf("Exact Jaccard: %.4f", exact)
	if exact < 0.7 {
		t.Skipf("test entries not similar enough (%.4f), adjust content", exact)
	}

	for i := range entries {
		entries[i].sig = MinHashSignature(entries[i].content, DefaultNumHashes)
	}

	// Even if LSH misses the pair, the near-miss sweep should find it.
	pairs := FindCandidatePairs(entries, 0.7, DefaultLSHBands)
	if len(pairs) == 0 {
		t.Error("near-miss sweep should have caught the similar pair")
	}
}

// Benchmarks are in benchmark_test.go to avoid duplication.
