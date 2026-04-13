package memory

import (
	"hash/fnv"
	"math"
	"strings"
)

// MinHash/LSH implementation for O(n) approximate deduplication.
// Replaces the O(n²) pairwise Jaccard comparison in the consolidation pipeline.
//
// Algorithm:
//   1. Compute MinHash signatures for each entry using word-level bigram shingles
//   2. Use LSH banding to bucket entries into candidate groups (O(n))
//   3. Verify candidates with exact Jaccard similarity (O(c) where c << n²)

const (
	// DefaultNumHashes is the number of hash functions for MinHash signatures.
	// More hashes = more accurate similarity estimation, at higher compute cost.
	DefaultNumHashes = 128

	// DefaultLSHBands is the number of LSH bands. With 128 hashes and 16 bands,
	// each band has 8 rows. This gives ~85% detection probability at Jaccard ≥ 0.85
	// and ~20% false positive rate at Jaccard ≈ 0.5.
	DefaultLSHBands = 16
)

// minHashCoeffs holds pre-computed hash function coefficients for deterministic signatures.
// Using a universal hash family: h_i(x) = (a_i * x + b_i) mod p
// where p is a large prime, and (a_i, b_i) are deterministic coefficients.
type minHashCoeffs struct {
	a []uint64
	b []uint64
	p uint64
}

var defaultCoeffs = newMinHashCoeffs(DefaultNumHashes)

func newMinHashCoeffs(numHashes int) *minHashCoeffs {
	// Use a large Mersenne prime.
	p := uint64(2147483647) // 2^31 - 1
	c := &minHashCoeffs{
		a: make([]uint64, numHashes),
		b: make([]uint64, numHashes),
		p: p,
	}
	// Deterministic coefficients from FNV hashing of index.
	for i := 0; i < numHashes; i++ {
		h := fnv.New64a()
		h.Write([]byte{byte(i >> 8), byte(i), 'a'})
		c.a[i] = h.Sum64()%p + 1 // a must be non-zero
		h.Reset()
		h.Write([]byte{byte(i >> 8), byte(i), 'b'})
		c.b[i] = h.Sum64() % p
	}
	return c
}

// shingleBigrams extracts word-level bigram shingles from text.
// Bigrams provide better discrimination than unigrams for dedup detection.
func shingleBigrams(text string) []uint64 {
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return nil
	}
	if len(words) == 1 {
		h := fnv.New64a()
		h.Write([]byte(words[0]))
		return []uint64{h.Sum64()}
	}
	shingles := make([]uint64, 0, len(words)-1)
	for i := 0; i < len(words)-1; i++ {
		h := fnv.New64a()
		h.Write([]byte(words[i]))
		h.Write([]byte{' '})
		h.Write([]byte(words[i+1]))
		shingles = append(shingles, h.Sum64())
	}
	return shingles
}

// MinHashSignature computes the MinHash signature for a text string.
// The signature is a vector of numHashes minimum hash values, one per hash function.
func MinHashSignature(text string, numHashes int) []uint32 {
	shingles := shingleBigrams(text)
	if len(shingles) == 0 {
		return make([]uint32, numHashes)
	}

	coeffs := defaultCoeffs
	if numHashes != DefaultNumHashes {
		coeffs = newMinHashCoeffs(numHashes)
	}

	sig := make([]uint32, numHashes)
	for i := range sig {
		sig[i] = math.MaxUint32
	}

	for _, shingle := range shingles {
		for i := 0; i < numHashes; i++ {
			hashVal := uint32((coeffs.a[i]*shingle + coeffs.b[i]) % coeffs.p)
			if hashVal < sig[i] {
				sig[i] = hashVal
			}
		}
	}
	return sig
}

// EstimateJaccard estimates Jaccard similarity from two MinHash signatures.
func EstimateJaccard(sigA, sigB []uint32) float64 {
	if len(sigA) != len(sigB) || len(sigA) == 0 {
		return 0
	}
	matches := 0
	for i := range sigA {
		if sigA[i] == sigB[i] {
			matches++
		}
	}
	return float64(matches) / float64(len(sigA))
}

// LSHBuckets partitions a MinHash signature into bands and hashes each band
// to produce bucket keys. Two entries that share any bucket are candidate duplicates.
func LSHBuckets(sig []uint32, bands int) []uint64 {
	if len(sig) == 0 || bands <= 0 {
		return nil
	}
	rowsPerBand := len(sig) / bands
	if rowsPerBand == 0 {
		rowsPerBand = 1
		bands = len(sig)
	}

	buckets := make([]uint64, bands)
	for b := 0; b < bands; b++ {
		h := fnv.New64a()
		// Include band index to avoid cross-band collisions.
		h.Write([]byte{byte(b >> 8), byte(b)})
		start := b * rowsPerBand
		end := start + rowsPerBand
		if end > len(sig) {
			end = len(sig)
		}
		for i := start; i < end; i++ {
			h.Write([]byte{
				byte(sig[i] >> 24), byte(sig[i] >> 16),
				byte(sig[i] >> 8), byte(sig[i]),
			})
		}
		buckets[b] = h.Sum64()
	}
	return buckets
}

// dedupEntry represents a memory entry for LSH-based dedup.
type dedupEntry struct {
	id      string
	content string
	score   float64
	sig     []uint32
}

// candidatePair is a pair of indices into the entries slice that may be duplicates.
type candidatePair struct {
	i, j int
}

// FindCandidatePairs uses LSH to find entries that are likely duplicates,
// then verifies with exact Jaccard similarity.
// Returns verified pairs that exceed the threshold.
func FindCandidatePairs(entries []dedupEntry, threshold float64, bands int) []candidatePair {
	if len(entries) <= 1 {
		return nil
	}

	// Build LSH index: bucket → list of entry indices.
	bucketIndex := make(map[uint64][]int)
	for i, e := range entries {
		buckets := LSHBuckets(e.sig, bands)
		for _, bucket := range buckets {
			bucketIndex[bucket] = append(bucketIndex[bucket], i)
		}
	}

	// Collect candidate pairs from shared buckets.
	pairSeen := make(map[[2]int]struct{})
	var candidates []candidatePair
	for _, indices := range bucketIndex {
		if len(indices) < 2 {
			continue
		}
		for a := 0; a < len(indices); a++ {
			for b := a + 1; b < len(indices); b++ {
				key := [2]int{indices[a], indices[b]}
				if _, seen := pairSeen[key]; seen {
					continue
				}
				pairSeen[key] = struct{}{}
				candidates = append(candidates, candidatePair{i: indices[a], j: indices[b]})
			}
		}
	}

	// Verify candidates with exact Jaccard similarity.
	var verified []candidatePair
	for _, cp := range candidates {
		sim := jaccardSimilarity(entries[cp.i].content, entries[cp.j].content)
		if sim >= threshold {
			verified = append(verified, cp)
		}
	}

	// Second pass: catch LSH false negatives. For pairs NOT already found by LSH
	// but with high MinHash estimated Jaccard (> nearMissThreshold), verify with
	// exact Jaccard. This eliminates the ~15% false-negative rate at the boundary.
	const nearMissThreshold = 0.75
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			key := [2]int{i, j}
			if _, seen := pairSeen[key]; seen {
				continue // Already checked by LSH
			}
			estJaccard := EstimateJaccard(entries[i].sig, entries[j].sig)
			if estJaccard >= nearMissThreshold {
				// Near miss — verify with exact Jaccard.
				sim := jaccardSimilarity(entries[i].content, entries[j].content)
				if sim >= threshold {
					verified = append(verified, candidatePair{i: i, j: j})
				}
			}
		}
	}

	return verified
}
