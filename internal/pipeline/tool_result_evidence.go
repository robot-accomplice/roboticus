package pipeline

import (
	"strings"

	agenttools "roboticus/internal/agent/tools"
)

func currentTurnToolResults(sess *Session) []ToolResultEntry {
	if sess == nil {
		return nil
	}
	msgs := sess.Messages()
	lastUserIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return nil
	}
	var results []ToolResultEntry
	for i := lastUserIdx + 1; i < len(msgs); i++ {
		if msgs[i].Role != "tool" {
			continue
		}
		entry := ToolResultEntry{
			ToolName: msgs[i].Name,
			Output:   msgs[i].Content,
			Metadata: msgs[i].Metadata,
		}
		if proof, ok := agenttools.ParseArtifactProof(msgs[i].Metadata); ok {
			entry.ArtifactProof = proof
		}
		results = append(results, entry)
	}
	return results
}

func toolResultEvidenceItems(results []ToolResultEntry) []string {
	var items []string
	for _, tr := range results {
		if tr.ArtifactProof == nil {
			continue
		}
		proof := tr.ArtifactProof
		item := "[tool_artifact, canonical] " + proof.ArtifactKind + " " + proof.Path +
			" sha256=" + proof.ContentSHA256
		if proof.ExactContentIncluded && strings.TrimSpace(proof.Content) != "" {
			item += " content=" + proof.Content
		} else if strings.TrimSpace(proof.ContentPreview) != "" {
			item += " preview=" + proof.ContentPreview
		}
		items = append(items, item)
	}
	return items
}

func toolResultArtifactProofs(results []ToolResultEntry) []agenttools.ArtifactProof {
	var proofs []agenttools.ArtifactProof
	for _, tr := range results {
		if tr.ArtifactProof == nil {
			continue
		}
		proofs = append(proofs, *tr.ArtifactProof)
	}
	return proofs
}

func contradictionChecksSuppressedByArtifactProof(ctx VerificationContext, content string) bool {
	if !persistentArtifactProofRequired(ctx.UserPrompt) {
		return false
	}
	if !responseClaimsPersistentArtifactMutation(content) {
		return false
	}
	return ctx.ArtifactConformance.AllExactSatisfied()
}
