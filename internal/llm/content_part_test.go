package llm

import "testing"

func TestTextPart(t *testing.T) {
	p := TextPart("hello")
	if p.Type != "text" {
		t.Errorf("type = %s, want text", p.Type)
	}
	if p.Text != "hello" {
		t.Errorf("text = %s", p.Text)
	}
}

func TestImageURLPart(t *testing.T) {
	p := ImageURLPart("https://example.com/img.png")
	if p.Type != "image_url" {
		t.Errorf("type = %s, want image_url", p.Type)
	}
	if p.URL != "https://example.com/img.png" {
		t.Errorf("url = %s", p.URL)
	}
	if p.Detail != "auto" {
		t.Errorf("detail = %s, want auto", p.Detail)
	}
}

func TestImageBase64Part(t *testing.T) {
	p := ImageBase64Part("image/png", "iVBOR...")
	if p.Type != "image_base64" {
		t.Errorf("type = %s, want image_base64", p.Type)
	}
	if p.MediaType != "image/png" {
		t.Errorf("media_type = %s", p.MediaType)
	}
}

func TestIsMultimodal(t *testing.T) {
	if IsMultimodal([]ContentPart{TextPart("hello")}) {
		t.Error("text-only should not be multimodal")
	}
	if !IsMultimodal([]ContentPart{TextPart("hello"), ImageURLPart("http://img")}) {
		t.Error("text+image should be multimodal")
	}
	if IsMultimodal(nil) {
		t.Error("nil should not be multimodal")
	}
}
