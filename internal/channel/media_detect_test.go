package channel

import "testing"

func TestDetectMediaType(t *testing.T) {
	tests := []struct {
		mime string
		want MediaType
	}{
		{"image/png", MediaImage},
		{"image/jpeg", MediaImage},
		{"IMAGE/GIF", MediaImage},
		{"audio/mp3", MediaAudio},
		{"audio/ogg", MediaAudio},
		{"Audio/WAV", MediaAudio},
		{"video/mp4", MediaVideo},
		{"video/webm", MediaVideo},
		{"VIDEO/AVI", MediaVideo},
		{"application/pdf", MediaDocument},
		{"text/plain", MediaDocument},
		{"application/json", MediaDocument},
		{"", MediaDocument},
		{"  image/png  ", MediaImage},
	}
	for _, tc := range tests {
		got := DetectMediaType(tc.mime)
		if got != tc.want {
			t.Errorf("DetectMediaType(%q) = %q, want %q", tc.mime, got, tc.want)
		}
	}
}
