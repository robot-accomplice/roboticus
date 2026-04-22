package tools

import "encoding/json"

// ArtifactReadProof is the typed proof emitted by successful artifact-reading
// tools. It is the authoritative evidence that a source artifact was actually
// read, not merely mentioned in the prompt.
type ArtifactReadProof struct {
	ProofType            string `json:"proof_type"`
	ArtifactKind         string `json:"artifact_kind"`
	Path                 string `json:"path"`
	Bytes                int    `json:"bytes"`
	ContentSHA256        string `json:"content_sha256"`
	ExactContentIncluded bool   `json:"exact_content_included"`
	Content              string `json:"content,omitempty"`
	ContentPreview       string `json:"content_preview,omitempty"`
	ContentTruncated     bool   `json:"content_truncated,omitempty"`
}

func NewArtifactReadProof(kind, path, content string) ArtifactReadProof {
	proof := ArtifactReadProof{
		ProofType:     "artifact_read",
		ArtifactKind:  kind,
		Path:          path,
		Bytes:         len(content),
		ContentSHA256: sha256Hex(content),
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

func (p ArtifactReadProof) Metadata() json.RawMessage {
	buf, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return json.RawMessage(buf)
}

func ParseArtifactReadProof(raw json.RawMessage) (*ArtifactReadProof, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var proof ArtifactReadProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return nil, false
	}
	if proof.ProofType != "artifact_read" || proof.Path == "" || proof.ArtifactKind == "" {
		return nil, false
	}
	return &proof, true
}
