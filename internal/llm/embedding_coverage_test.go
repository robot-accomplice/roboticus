package llm

import "testing"

func TestNewEmbeddingClient(t *testing.T) {
	ec := NewEmbeddingClient(&Provider{
		Name:           "test",
		URL:            "http://localhost:11434",
		Format:         FormatOllama,
		EmbeddingModel: "nomic-embed-text",
		IsLocal:        true,
	})
	if ec == nil {
		t.Fatal("nil")
	}
	dims := ec.Dimensions()
	if dims <= 0 {
		t.Errorf("dimensions = %d", dims)
	}
}

func TestEmbeddingClient_Dimensions_Default(t *testing.T) {
	ec := NewEmbeddingClient(&Provider{Name: "test", Format: FormatOpenAI})
	if ec.Dimensions() <= 0 {
		t.Error("default dimensions should be positive")
	}
}
