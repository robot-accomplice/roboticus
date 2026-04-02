package llm

// ContentPart represents a single element of a multimodal message.
// When a Message has non-nil ContentParts, they take precedence over Content.
type ContentPart struct {
	Type string `json:"type"` // "text", "image_url", "image_base64"

	// Text content (type="text").
	Text string `json:"text,omitempty"`

	// Image URL (type="image_url").
	URL    string `json:"url,omitempty"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"

	// Inline base64 image (type="image_base64").
	MediaType string `json:"media_type,omitempty"` // e.g., "image/png"
	Data      string `json:"data,omitempty"`       // base64-encoded
}

// TextPart creates a text content part.
func TextPart(text string) ContentPart {
	return ContentPart{Type: "text", Text: text}
}

// ImageURLPart creates an image URL content part.
func ImageURLPart(url string) ContentPart {
	return ContentPart{Type: "image_url", URL: url, Detail: "auto"}
}

// ImageBase64Part creates an inline base64 image content part.
func ImageBase64Part(mediaType, data string) ContentPart {
	return ContentPart{Type: "image_base64", MediaType: mediaType, Data: data}
}

// IsMultimodal returns true if the parts contain any non-text content.
func IsMultimodal(parts []ContentPart) bool {
	for _, p := range parts {
		if p.Type != "text" {
			return true
		}
	}
	return false
}
