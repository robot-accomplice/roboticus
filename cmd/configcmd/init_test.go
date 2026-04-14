package configcmd

import (
	"strings"
	"testing"
)

func TestDefaultConfigTOML_Contents(t *testing.T) {
	if !strings.Contains(defaultConfigTOML, "[agent]") {
		t.Error("default config should contain [agent] section")
	}
	if !strings.Contains(defaultConfigTOML, "[server]") {
		t.Error("default config should contain [server] section")
	}
	if !strings.Contains(defaultConfigTOML, "[database]") {
		t.Error("default config should contain [database] section")
	}
	if !strings.Contains(defaultConfigTOML, "[models]") {
		t.Error("default config should contain [models] section")
	}
	if !strings.Contains(defaultConfigTOML, "[memory]") {
		t.Error("default config should contain [memory] section")
	}
	if !strings.Contains(defaultConfigTOML, "[security]") {
		t.Error("default config should contain [security] section")
	}
	if !strings.Contains(defaultConfigTOML, "port = 18789") {
		t.Error("default config should have port 18789 (Rust parity)")
	}
}
