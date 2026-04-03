package policy

import "testing"

func TestApprovalManager_ClassifyTool(t *testing.T) {
	mgr := NewApprovalManager(ApprovalsConfig{
		Enabled:      true,
		GatedTools:   []string{"bash", "write_file"},
		BlockedTools: []string{"rm_rf"},
	})

	tests := []struct {
		tool string
		want ToolClassification
	}{
		{"echo", ToolSafe},
		{"bash", ToolGated},
		{"write_file", ToolGated},
		{"rm_rf", ToolBlocked},
	}
	for _, tt := range tests {
		got := mgr.ClassifyTool(tt.tool)
		if got != tt.want {
			t.Errorf("ClassifyTool(%q) = %v, want %v", tt.tool, got, tt.want)
		}
	}
}

func TestApprovalManager_ClassifyTool_GatedRegardless(t *testing.T) {
	// ClassifyTool always classifies based on lists; Enabled is a caller concern.
	mgr := NewApprovalManager(ApprovalsConfig{
		Enabled:    false,
		GatedTools: []string{"bash"},
	})
	got := mgr.ClassifyTool("bash")
	if got != ToolGated {
		t.Errorf("gated tool classified as %v regardless of Enabled", got)
	}
}

func TestApprovalManager_RequestAndApprove(t *testing.T) {
	mgr := NewApprovalManager(ApprovalsConfig{
		Enabled:        true,
		GatedTools:     []string{"bash"},
		TimeoutSeconds: 60,
	})

	req := mgr.RequestApproval("req1", "bash", `{"cmd":"ls"}`, "session1", "turn1")
	if req == nil {
		t.Fatal("should return approval request")
	}

	pending := mgr.ListPending()
	if len(pending) != 1 {
		t.Errorf("pending = %d, want 1", len(pending))
	}

	err := mgr.Approve("req1", "admin")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	pending = mgr.ListPending()
	if len(pending) != 0 {
		t.Errorf("pending after approve = %d, want 0", len(pending))
	}
}

func TestApprovalManager_RequestAndDeny(t *testing.T) {
	mgr := NewApprovalManager(ApprovalsConfig{
		Enabled:        true,
		GatedTools:     []string{"bash"},
		TimeoutSeconds: 60,
	})

	mgr.RequestApproval("req2", "bash", `{"cmd":"rm -rf /"}`, "s1", "t1")
	err := mgr.Deny("req2", "admin", "too dangerous")
	if err != nil {
		t.Fatalf("deny: %v", err)
	}

	all := mgr.ListAll()
	found := false
	for _, a := range all {
		if a.ID == "req2" && a.Status == "denied" {
			found = true
		}
	}
	if !found {
		t.Error("denied approval not found in list")
	}
}

func TestApprovalManager_Get(t *testing.T) {
	mgr := NewApprovalManager(ApprovalsConfig{
		Enabled:        true,
		GatedTools:     []string{"bash"},
		TimeoutSeconds: 60,
	})

	mgr.RequestApproval("req3", "bash", `{}`, "s1", "t1")
	approval := mgr.Get("req3")
	if approval == nil {
		t.Fatal("should find approval")
	}
	if approval.ToolName != "bash" {
		t.Errorf("tool = %s", approval.ToolName)
	}
}

func TestApprovalManager_Get_NotFound(t *testing.T) {
	mgr := NewApprovalManager(ApprovalsConfig{Enabled: true})
	approval := mgr.Get("nonexistent")
	if approval != nil {
		t.Error("should return nil for nonexistent")
	}
}

func TestApprovalManager_ExpireTimedOut(t *testing.T) {
	mgr := NewApprovalManager(ApprovalsConfig{
		Enabled:        true,
		GatedTools:     []string{"bash"},
		TimeoutSeconds: 1,
	})

	mgr.RequestApproval("req4", "bash", `{}`, "s1", "t1")
	mgr.ExpireTimedOut() // Exercise the code path. With 1s timeout, may not expire yet.
	// Just verify it doesn't panic and ListPending works after.
	_ = mgr.ListPending()
}

func TestApprovalManager_JSON(t *testing.T) {
	mgr := NewApprovalManager(ApprovalsConfig{
		Enabled:        true,
		GatedTools:     []string{"bash"},
		TimeoutSeconds: 60,
	})

	mgr.RequestApproval("req5", "bash", `{}`, "s1", "t1")

	if len(mgr.ListAllJSON()) != 1 {
		t.Errorf("ListAllJSON = %d", len(mgr.ListAllJSON()))
	}
	if len(mgr.ListPendingJSON()) != 1 {
		t.Errorf("ListPendingJSON = %d", len(mgr.ListPendingJSON()))
	}
	if mgr.GetJSON("req5") == nil {
		t.Error("GetJSON should return non-nil")
	}
}

func TestApprovalManager_ClearDecided(t *testing.T) {
	mgr := NewApprovalManager(ApprovalsConfig{
		Enabled:        true,
		GatedTools:     []string{"bash"},
		TimeoutSeconds: 60,
	})

	mgr.RequestApproval("req6", "bash", `{}`, "s1", "t1")
	_ = mgr.Approve("req6", "admin")
	mgr.ClearDecided()

	if len(mgr.ListAll()) != 0 {
		t.Errorf("all after clear = %d", len(mgr.ListAll()))
	}
}
