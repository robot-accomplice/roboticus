package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const maxExactArtifactContentBytes = 2048

// ArtifactProof is the typed proof emitted by successful artifact-writing
// tools. It is the authoritative post-write evidence consumed by session
// history, guards, verifier, and RCA.
type ArtifactProof struct {
	ProofType            string `json:"proof_type"`
	ArtifactKind         string `json:"artifact_kind"`
	Path                 string `json:"path"`
	Bytes                int    `json:"bytes"`
	ContentSHA256        string `json:"content_sha256"`
	Append               bool   `json:"append"`
	ExactContentIncluded bool   `json:"exact_content_included"`
	Content              string `json:"content,omitempty"`
	ContentPreview       string `json:"content_preview,omitempty"`
	ContentTruncated     bool   `json:"content_truncated,omitempty"`
}

func NewArtifactProof(kind, path, content string, appendMode bool) ArtifactProof {
	proof := ArtifactProof{
		ProofType:     "artifact_write",
		ArtifactKind:  kind,
		Path:          path,
		Bytes:         len(content),
		ContentSHA256: sha256Hex(content),
		Append:        appendMode,
	}
	if len(content) <= maxExactArtifactContentBytes {
		proof.ExactContentIncluded = true
		proof.Content = content
		return proof
	}
	proof.ContentPreview = content[:maxExactArtifactContentBytes]
	proof.ContentTruncated = true
	return proof
}

func (p ArtifactProof) Metadata() json.RawMessage {
	buf, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return json.RawMessage(buf)
}

func (p ArtifactProof) Output() string {
	buf, err := json.Marshal(p)
	if err != nil {
		return `{"proof_type":"artifact_write","artifact_kind":"` + p.ArtifactKind + `","path":"` + p.Path + `"}`
	}
	return string(buf)
}

func ParseArtifactProof(raw json.RawMessage) (*ArtifactProof, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var proof ArtifactProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return nil, false
	}
	if proof.ProofType != "artifact_write" || proof.Path == "" || proof.ArtifactKind == "" {
		return nil, false
	}
	return &proof, true
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
