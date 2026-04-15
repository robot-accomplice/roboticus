// verifier_classifier.go ships the embedding-backed semantic claim
// certainty classifier (Milestone 6 follow-on).
//
// The classifier is the SECOND-LINE certainty check. The lexical markers in
// verifier_claims.go (absoluteMarkers / hedgeMarkers / highCertaintyMarkers)
// run first because they are 100% precision for known phrases and they cost
// nothing. Sentences that none of those markers match flow through to the
// classifier so paraphrased certainty cues — "by no means could this fail",
// "we are sceptical that this works", "the resolution is final", etc. — are
// still correctly tagged.
//
// The corpus below is intentionally small and adversarial. It is not a
// linguistic benchmark; it is a regression asset. Each example is chosen
// because it represents a verifier failure mode we already care about:
//   - absolute certainty asserted without evidence ("the answer is final")
//   - pseudo-cautious wording that still closes a question
//     ("there is no doubt this is true")
//   - policy / currentness overclaim ("this is now policy")
//   - remediation phrased as fact ("the fix is to ...")
//   - softened hallucinations that nonetheless resolve the goal
//     ("rest assured the rollout completed")
//
// When the classifier returns "abstain" (low confidence or near-tie), we do
// NOT downgrade the sentence — we let it default to CertaintyModerate, which
// is the verifier's existing "no marker matched" behaviour. That keeps the
// classifier from generating new false positives just because the embedding
// was ambiguous.

package pipeline

import (
	"context"

	"roboticus/internal/llm"
)

// claimCertaintyExamples is the curated exemplar corpus. Keep examples
// short and unambiguous — embeddings work best on punchy, distinctive
// phrasing. Add new examples here when you see a real verifier miss.
//
// CONVENTION: when adding a new example, prefer phrasings that the lexical
// markers in verifier_claims.go would NOT catch — that is where the
// classifier earns its keep.
func claimCertaintyExamples() []llm.ClassifierExample {
	return []llm.ClassifierExample{
		// --- absolute: certainty asserted without evidence ---
		{Intent: "absolute", Text: "by no means could this fail"},
		{Intent: "absolute", Text: "there is no doubt this is true"},
		{Intent: "absolute", Text: "this happens every single time without fail"},
		{Intent: "absolute", Text: "the answer is final and not up for debate"},
		{Intent: "absolute", Text: "rest assured the rollout completed cleanly"},
		{Intent: "absolute", Text: "the fix is straightforward and will work"},
		{Intent: "absolute", Text: "this is now policy across the entire platform"},
		{Intent: "absolute", Text: "the resolution is final and applies universally"},

		// --- high: assertive, but not making a universal claim ---
		{Intent: "high", Text: "the deploy was rolled back successfully"},
		{Intent: "high", Text: "the request returned a 200 response"},
		{Intent: "high", Text: "the migration ran to completion this morning"},
		{Intent: "high", Text: "the user opened the ticket yesterday"},
		{Intent: "high", Text: "the agent recorded a successful turn"},
		{Intent: "high", Text: "the configuration file currently lists three brokers"},
		{Intent: "high", Text: "the canary survived the smoke test"},

		// --- hedged: explicit uncertainty or reduced confidence ---
		{Intent: "hedged", Text: "we are sceptical that this works as intended"},
		{Intent: "hedged", Text: "this could be off; we have not confirmed it"},
		{Intent: "hedged", Text: "i don't have strong evidence either way"},
		{Intent: "hedged", Text: "it's plausible but unverified"},
		{Intent: "hedged", Text: "we'd want to double-check before relying on this"},
		{Intent: "hedged", Text: "the available evidence is thin and may be wrong"},
		{Intent: "hedged", Text: "this looks right at a glance but warrants review"},
		{Intent: "hedged", Text: "i would not stake a decision on this without more data"},
	}
}

// NewClaimCertaintyClassifier constructs the classifier with the curated
// exemplar corpus and pre-embeds every example so the first verifier call
// does not pay the corpus-embedding cost. embedder may be nil — the
// underlying SemanticClassifier transparently falls back to local n-gram
// embeddings, which beat pure lexical matching on paraphrases even without
// a network call.
//
// The abstain policy is intentionally conservative: a top score below 0.30
// or a margin under 0.10 over the runner-up returns "abstain", which the
// caller maps to CertaintyModerate so the classifier never invents
// certainty out of an ambiguous embedding.
//
// PrepareCorpus errors are ignored on purpose — n-gram fallback cannot
// fail, so a non-nil error here would only ever come from a remote
// embedder rejecting every example, in which case the classifier is still
// usable on subsequent calls (which transparently retry the embedder
// per-query) and we don't want construction to fail just because the
// remote was momentarily down.
func NewClaimCertaintyClassifier(embedder *llm.EmbeddingClient) *llm.SemanticClassifier {
	classifier := llm.NewSemanticClassifier(embedder, claimCertaintyExamples())
	classifier.WithAbstainPolicy(llm.AbstainPolicy{MinScore: 0.30, MinGap: 0.10})
	_, _ = classifier.PrepareCorpus(context.Background())
	return classifier
}

// classifyCertaintySemantic runs the semantic certainty classifier and
// translates its string label into a ClaimCertainty. Returns
// (CertaintyModerate, false) when the classifier is nil, when classify
// returns "abstain", or on any error — so the caller gets the same default
// they would have gotten from purely lexical analysis.
func classifyCertaintySemantic(ctx context.Context, classifier *llm.SemanticClassifier, sentence string) (ClaimCertainty, bool) {
	if classifier == nil || sentence == "" {
		return CertaintyModerate, false
	}
	intent, _, err := classifier.Classify(ctx, sentence)
	if err != nil || intent == "abstain" || intent == "unknown" {
		return CertaintyModerate, false
	}
	switch intent {
	case "absolute":
		return CertaintyAbsolute, true
	case "high":
		return CertaintyHigh, true
	case "hedged":
		return CertaintyHedged, true
	default:
		return CertaintyModerate, false
	}
}
