package agent

import (
	"testing"
)

func makeTestManifest(name string) AppManifest {
	return AppManifest{
		Package: AppPackage{
			Name:        name,
			Version:     "1.0.0",
			Description: "A test app",
			Author:      "Test Author",
		},
		Profile: AppProfile{
			AgentName:    "TestAgent",
			AgentID:      "test-agent-id",
			DefaultTheme: "dark",
		},
		Requirements: AppRequirements{
			MinModelParams:    "7B",
			RecommendedModel:  "gpt-4",
			DelegationEnabled: true,
		},
	}
}

func TestAppManager_InstallFromMemory(t *testing.T) {
	am := NewAppManager("/tmp/test-apps")
	manifest := makeTestManifest("my-app")

	app, err := am.InstallFromMemory(manifest, "/tmp/test-apps/my-app")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if app.Manifest.Package.Name != "my-app" {
		t.Errorf("expected name 'my-app', got %q", app.Manifest.Package.Name)
	}
	if !app.Enabled {
		t.Error("expected app to be enabled after install")
	}
	if app.Path != "/tmp/test-apps/my-app" {
		t.Errorf("expected path '/tmp/test-apps/my-app', got %q", app.Path)
	}
}

func TestAppManager_DuplicateInstallError(t *testing.T) {
	am := NewAppManager("/tmp/test-apps")
	manifest := makeTestManifest("dup-app")

	if _, err := am.InstallFromMemory(manifest, "/tmp"); err != nil {
		t.Fatalf("first install failed: %v", err)
	}

	_, err := am.InstallFromMemory(manifest, "/tmp")
	if err == nil {
		t.Fatal("expected error on duplicate install, got nil")
	}
}

func TestAppManager_MissingNameError(t *testing.T) {
	am := NewAppManager("/tmp/test-apps")
	manifest := AppManifest{} // empty name

	_, err := am.InstallFromMemory(manifest, "/tmp")
	if err == nil {
		t.Fatal("expected error for missing package.name, got nil")
	}
}

func TestAppManager_Uninstall(t *testing.T) {
	am := NewAppManager("/tmp/test-apps")
	manifest := makeTestManifest("removable-app")

	if _, err := am.InstallFromMemory(manifest, "/tmp"); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	if removed := am.Uninstall("removable-app"); !removed {
		t.Fatal("expected Uninstall to return true")
	}

	if removed := am.Uninstall("removable-app"); removed {
		t.Fatal("expected Uninstall to return false for already-removed app")
	}
}

func TestAppManager_List(t *testing.T) {
	am := NewAppManager("/tmp/test-apps")

	for _, name := range []string{"app-alpha", "app-beta", "app-gamma"} {
		if _, err := am.InstallFromMemory(makeTestManifest(name), "/tmp/"+name); err != nil {
			t.Fatalf("install %q failed: %v", name, err)
		}
	}

	apps := am.List()
	if len(apps) != 3 {
		t.Errorf("expected 3 apps, got %d", len(apps))
	}
}

func TestAppManager_Get(t *testing.T) {
	am := NewAppManager("/tmp/test-apps")
	manifest := makeTestManifest("get-me")
	if _, err := am.InstallFromMemory(manifest, "/tmp"); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	app, ok := am.Get("get-me")
	if !ok {
		t.Fatal("expected Get to find installed app")
	}
	if app.Manifest.Package.Name != "get-me" {
		t.Errorf("unexpected app name: %q", app.Manifest.Package.Name)
	}

	_, ok = am.Get("nonexistent")
	if ok {
		t.Fatal("expected Get to return false for missing app")
	}
}

func TestAppManager_SetEnabled(t *testing.T) {
	am := NewAppManager("/tmp/test-apps")
	manifest := makeTestManifest("toggle-app")
	if _, err := am.InstallFromMemory(manifest, "/tmp"); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// disable
	if err := am.SetEnabled("toggle-app", false); err != nil {
		t.Fatalf("SetEnabled(false) failed: %v", err)
	}
	app, _ := am.Get("toggle-app")
	if app.Enabled {
		t.Error("expected app to be disabled")
	}

	// re-enable
	if err := am.SetEnabled("toggle-app", true); err != nil {
		t.Fatalf("SetEnabled(true) failed: %v", err)
	}
	app, _ = am.Get("toggle-app")
	if !app.Enabled {
		t.Error("expected app to be enabled")
	}

	// nonexistent
	if err := am.SetEnabled("ghost-app", true); err == nil {
		t.Fatal("expected error for nonexistent app, got nil")
	}
}
