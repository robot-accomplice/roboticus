package mcp

import "testing"

func TestMigrateLegacyClients_Empty(t *testing.T) {
	result := MigrateLegacyClients(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}

	result2 := MigrateLegacyClients([]LegacyClientConfig{})
	if result2 != nil {
		t.Fatalf("expected nil, got %v", result2)
	}
}

func TestMigrateLegacyClients_StdioClient(t *testing.T) {
	clients := []LegacyClientConfig{
		{
			Name:      "my-tool",
			Transport: "stdio",
			Command:   "/usr/bin/my-tool",
			Args:      []string{"--flag"},
			Env:       map[string]string{"KEY": "val"},
		},
	}
	servers := MigrateLegacyClients(clients)
	if len(servers) != 1 {
		t.Fatalf("expected 1, got %d", len(servers))
	}
	s := servers[0]
	if s.Name != "my-tool" || s.Transport != "stdio" || s.Command != "/usr/bin/my-tool" {
		t.Fatalf("bad server: %+v", s)
	}
	if !s.Enabled {
		t.Fatal("expected enabled")
	}
}

func TestMigrateLegacyClients_InferTransport(t *testing.T) {
	clients := []LegacyClientConfig{
		{Name: "cmd-tool", Command: "/bin/foo"},
		{Name: "sse-tool", URL: "http://localhost:9999"},
	}
	servers := MigrateLegacyClients(clients)
	if len(servers) != 2 {
		t.Fatalf("expected 2, got %d", len(servers))
	}
	if servers[0].Transport != "stdio" {
		t.Fatalf("expected stdio, got %s", servers[0].Transport)
	}
	if servers[1].Transport != "sse" {
		t.Fatalf("expected sse, got %s", servers[1].Transport)
	}
}

func TestMigrateLegacyClients_SkipsEmptyName(t *testing.T) {
	clients := []LegacyClientConfig{
		{Name: "", Command: "/bin/foo"},
	}
	servers := MigrateLegacyClients(clients)
	if len(servers) != 0 {
		t.Fatalf("expected 0, got %d", len(servers))
	}
}

func TestMigrateLegacyClients_SkipsNoTransport(t *testing.T) {
	clients := []LegacyClientConfig{
		{Name: "mystery"},
	}
	servers := MigrateLegacyClients(clients)
	if len(servers) != 0 {
		t.Fatalf("expected 0, got %d", len(servers))
	}
}
