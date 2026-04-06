package api

import (
	"testing"
)

func TestStatusTitle_KnownCodes(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{200, "OK"},
		{201, "Created"},
		{400, "Bad Request"},
		{401, "Unauthorized"},
		{403, "Forbidden"},
		{404, "Not Found"},
		{429, "Too Many Requests"},
		{500, "Internal Server Error"},
		{503, "Service Unavailable"},
	}
	for _, tt := range tests {
		got := StatusTitle(tt.code)
		if got != tt.want {
			t.Errorf("StatusTitle(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestStatusTitle_UnknownCode(t *testing.T) {
	got := StatusTitle(999)
	if got != "Unknown Error" {
		t.Errorf("StatusTitle(999) = %q, want 'Unknown Error'", got)
	}
}
