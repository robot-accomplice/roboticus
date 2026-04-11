package pipeline

import (
	"context"
	"strings"

	"roboticus/internal/llm"
)

// GuardExemplarSet defines a named category of exemplar phrases for semantic
// classification. The classifier computes embedding centroids from these
// exemplars and compares them against agent output.
// Rust parity: intent_exemplars.rs ExemplarSet + GUARD_EXEMPLARS.
type GuardExemplarSet struct {
	Name      string
	Exemplars []string
	Threshold float64 // minimum score to consider a match
}

// guardExemplars matches Rust's GUARD_EXEMPLARS exactly — 5 categories.
var guardExemplars = []GuardExemplarSet{
	{
		Name: "NARRATED_DELEGATION",
		Exemplars: []string{
			"let me delegate this to a specialist",
			"I'll hand this off to a subagent",
			"I have a specialist who handles this",
			"routing this to the lore_keeper",
			"I'll compose a specialist agent for this",
			"I'm going to delegate this task",
			"let me pass this to the right specialist",
			"I'll route this to the appropriate agent",
			"I'll have a subagent take care of that",
			"I'm delegating this to a specialist now",
			"handing this off to a subagent now",
			"I'm unable to handle this so routing to a subagent",
		},
		Threshold: 0.65,
	},
	{
		Name: "CAPABILITY_DENIAL",
		Exemplars: []string{
			"I can't access your files",
			"I don't have access to your filesystem",
			"I'm unable to run commands on your system",
			"I cannot browse the internet",
			"I don't have the ability to do that",
			"that's outside my capabilities",
			"I'm not able to interact with your system",
			"I have no way to access those files",
			"I cannot execute code on your machine",
			"I don't have tools to accomplish that",
		},
		Threshold: 0.65,
	},
	{
		Name: "TASK_DEFERRAL",
		Exemplars: []string{
			"Let me check that for you now",
			"I'll look into this right away",
			"I need to first inspect the current state",
			"I'm going to compose the necessary specialists for this task",
			"Next I can run a scan to verify",
			"I will set up the monitoring system shortly",
			"First, let me review the configuration",
			"I'll need to analyze the logs before I can answer",
			"Allow me to investigate this further",
			"I can then proceed to implement the solution",
		},
		Threshold: 0.70,
	},
	{
		Name: "FALSE_COMPLETION",
		Exemplars: []string{
			"I've scheduled the daily report for 8am",
			"The cron job has been created and is now active",
			"I have set up automated monitoring on your infrastructure",
			"Your daily briefing system is now operational",
			"I've configured the alerting pipeline",
			"The security scanner is deployed and running",
			"I have established the monitoring infrastructure",
			"The automated system is now live and protecting your stack",
			"All detection rules are verified and active",
			"I've activated the sentinel monitoring system",
		},
		Threshold: 0.70,
	},
	{
		Name: "FINANCIAL_ACTION_CLAIM",
		Exemplars: []string{
			"I've executed the trade successfully",
			"The funds have been transferred to your wallet",
			"Transaction confirmed on-chain",
			"Position opened at the specified price",
			"Swap completed, tokens received in your wallet",
			"I've sent 50 USDC to the target address",
			"The transfer is complete and confirmed",
			"Trade executed: bought 0.5 ETH at market price",
			"Your sell order has been filled",
			"I've moved the funds to the new wallet",
			"The deposit has been processed and credited",
			"Withdrawal complete, funds are in transit",
		},
		Threshold: 0.65,
	},
}

// PrecomputeGuardScores computes semantic similarity scores for all guard
// categories against the given response text.
// Rust parity: guard_registry.rs precompute_guard_scores().
//
// Uses the embedding client to embed the response and each exemplar set's
// centroid, then returns cosine similarity scores per category.
// When no embedding provider is configured, falls back to n-gram embeddings.
func PrecomputeGuardScores(ctx context.Context, ec *llm.EmbeddingClient, responseText string) map[string]float64 {
	scores := make(map[string]float64, len(guardExemplars))

	if ec == nil || strings.TrimSpace(responseText) == "" {
		return scores
	}

	// Embed the response text.
	responseVec, err := ec.EmbedSingle(ctx, responseText)
	if err != nil {
		return scores
	}

	// For each guard category, compute centroid of exemplars and compare.
	for _, cat := range guardExemplars {
		centroid := computeCentroid(ctx, ec, cat.Exemplars)
		if centroid == nil {
			continue
		}
		sim := llm.CosineSimilarity(responseVec, centroid)
		scores[cat.Name] = sim
	}

	return scores
}

// computeCentroid embeds all exemplars and returns their element-wise mean vector.
func computeCentroid(ctx context.Context, ec *llm.EmbeddingClient, phrases []string) []float32 {
	if len(phrases) == 0 {
		return nil
	}

	var vecs [][]float32
	for _, phrase := range phrases {
		v, err := ec.EmbedSingle(ctx, phrase)
		if err != nil {
			continue
		}
		vecs = append(vecs, v)
	}

	if len(vecs) == 0 {
		return nil
	}

	dim := len(vecs[0])
	centroid := make([]float32, dim)
	for _, v := range vecs {
		if len(v) != dim {
			continue
		}
		for i := range centroid {
			centroid[i] += v[i]
		}
	}
	n := float32(len(vecs))
	for i := range centroid {
		centroid[i] /= n
	}
	return centroid
}

// GuardScoreAboveThreshold returns true if the semantic score for a given
// category exceeds its configured threshold.
func GuardScoreAboveThreshold(scores map[string]float64, category string) bool {
	score, ok := scores[category]
	if !ok {
		return false
	}
	for _, cat := range guardExemplars {
		if cat.Name == category {
			return score >= cat.Threshold
		}
	}
	return false
}
