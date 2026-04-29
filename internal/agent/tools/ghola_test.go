package tools

import (
	"encoding/json"
	"testing"
)

func TestGholaValidateTargetURL_RejectsPrivateHost(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/foo",
		"http://localhost/",
		"http://192.168.1.1/",
	} {
		if _, err := gholaValidateTargetURL(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestGholaValidateTargetURL_AcceptsHTTPS(t *testing.T) {
	u, err := gholaValidateTargetURL("https://example.com/path")
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "example.com" {
		t.Fatalf("host = %q", u.Hostname())
	}
}

func TestGholaArgv_URLLast(t *testing.T) {
	argv := gholaArgv("POST", map[string]string{"Content-Type": "application/json"}, `{"a":1}`, "https://api.example.com/x", 30000, "", false)
	if len(argv) < 2 {
		t.Fatalf("argv too short: %v", argv)
	}
	if got := argv[len(argv)-1]; got != "https://api.example.com/x" {
		t.Fatalf("last arg = %q, want URL", got)
	}
	var sawL, sawTimeout bool
	for i, a := range argv {
		if a == "-L" {
			sawL = true
		}
		if a == "--timeout" && i+1 < len(argv) && argv[i+1] == "30000" {
			sawTimeout = true
		}
	}
	if !sawL {
		t.Fatalf("expected -L in argv: %v", argv)
	}
	if !sawTimeout {
		t.Fatalf("expected --timeout 30000 in argv: %v", argv)
	}
	for _, a := range argv {
		if a == "--impersonate" {
			t.Fatalf("API-style POST should not impersonate: %v", argv)
		}
		if a == "-i" {
			t.Fatalf("API-style POST should not use -i: %v", argv)
		}
	}
}

func TestGholaArgv_DocumentGETUsesDerivedBrowserProfile(t *testing.T) {
	argv := gholaArgv("", nil, "", "https://example.com/", 5000, "chrome", true)
	var sawImpersonate, sawI bool
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "--impersonate" && argv[i+1] == "chrome" {
			sawImpersonate = true
		}
		if argv[i] == "-i" {
			sawI = true
		}
	}
	if !sawImpersonate || !sawI {
		t.Fatalf("expected --impersonate chrome and -i for document GET: %v", argv)
	}
}

func TestGholaDeriveClientOpts(t *testing.T) {
	imp, hdr := gholaDeriveClientOpts("", "")
	if imp != "chrome" || !hdr {
		t.Fatalf("plain GET: got %q %v", imp, hdr)
	}
	imp, hdr = gholaDeriveClientOpts("GET", "")
	if imp != "chrome" || !hdr {
		t.Fatalf("explicit GET: got %q %v", imp, hdr)
	}
	imp, hdr = gholaDeriveClientOpts("POST", `{"x":1}`)
	if imp != "" || hdr {
		t.Fatalf("POST+body: got %q %v want empty,false", imp, hdr)
	}
	imp, hdr = gholaDeriveClientOpts("GET", "x=1")
	if imp != "" || hdr {
		t.Fatalf("GET+body: got %q %v", imp, hdr)
	}
	imp, hdr = gholaDeriveClientOpts("HEAD", "")
	if imp != "chrome" || !hdr {
		t.Fatalf("HEAD: got %q %v", imp, hdr)
	}
}

func TestGholaTool_ParameterSchemaIsJSON(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(NewGholaTool("").ParameterSchema(), &m); err != nil {
		t.Fatal(err)
	}
}
