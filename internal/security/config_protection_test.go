package security

import "testing"

func TestReferencesProtectedConfigFile(t *testing.T) {
	if !ReferencesProtectedConfigFile(`{"path":"roboticus.toml"}`) {
		t.Fatal("expected roboticus.toml to be recognized as protected config")
	}
	if ReferencesProtectedConfigFile(`{"path":"notes.txt"}`) {
		t.Fatal("did not expect notes.txt to be recognized as protected config")
	}
}

func TestMatchProtectedConfigPattern(t *testing.T) {
	tests := []struct {
		text string
		want string
		ok   bool
	}{
		{`wallet.keyfile="/tmp/key.json"`, "wallet.keyfile", true},
		{`server.auth_token="abc"`, "server.auth_token", true},
		{`db_secret="s3cr3t"`, "*_secret", true},
		{`refresh_token="tok"`, "*_token", true},
		{`agent_name="bot"`, "", false},
	}

	for _, tt := range tests {
		got, ok := MatchProtectedConfigPattern(tt.text)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("MatchProtectedConfigPattern(%q) = (%q, %v), want (%q, %v)", tt.text, got, ok, tt.want, tt.ok)
		}
	}
}
