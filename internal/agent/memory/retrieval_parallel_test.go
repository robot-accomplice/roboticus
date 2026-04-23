package memory

import (
	"context"
	"testing"
	"time"
)

func TestRunTierRetrievalJobs_ParallelAndDeterministic(t *testing.T) {
	jobs := []tierRetrievalJob{
		{SubgoalIndex: 0, TargetIndex: 0, Question: "first", Target: RetrievalTarget{Tier: TierSemantic}},
		{SubgoalIndex: 0, TargetIndex: 1, Question: "second", Target: RetrievalTarget{Tier: TierEpisodic}},
		{SubgoalIndex: 1, TargetIndex: 0, Question: "third", Target: RetrievalTarget{Tier: TierProcedural}},
	}
	delays := map[string]time.Duration{
		"first":  140 * time.Millisecond,
		"second": 20 * time.Millisecond,
		"third":  70 * time.Millisecond,
	}

	start := time.Now()
	results := runTierRetrievalJobs(context.Background(), jobs, func(_ context.Context, job tierRetrievalJob) []Evidence {
		time.Sleep(delays[job.Question])
		return []Evidence{{Content: job.Question}}
	})
	elapsed := time.Since(start)

	if got, want := len(results), len(jobs); got != want {
		t.Fatalf("results len = %d, want %d", got, want)
	}
	// Sequential execution would take ~230ms. Keep a generous threshold so this
	// remains stable on slower CI while still proving the fan-out is concurrent.
	if elapsed >= 210*time.Millisecond {
		t.Fatalf("parallel retrieval took %v, want substantially less than sequential ~230ms", elapsed)
	}

	gotOrder := []string{
		results[0].Question,
		results[1].Question,
		results[2].Question,
	}
	wantOrder := []string{"first", "second", "third"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("result[%d] question = %q, want %q (router order must be preserved after concurrent fetch)", i, gotOrder[i], wantOrder[i])
		}
	}
}
