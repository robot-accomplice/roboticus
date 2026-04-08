package channel

import "strings"

// DetectMediaType infers a MediaType from a MIME type string.
// It examines the MIME prefix: "image/" -> MediaImage, "audio/" -> MediaAudio,
// "video/" -> MediaVideo. Everything else maps to MediaDocument.
func DetectMediaType(mimeType string) MediaType {
	lower := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(lower, "image/"):
		return MediaImage
	case strings.HasPrefix(lower, "audio/"):
		return MediaAudio
	case strings.HasPrefix(lower, "video/"):
		return MediaVideo
	default:
		return MediaDocument
	}
}
