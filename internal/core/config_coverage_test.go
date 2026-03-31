package core

import "testing"

func TestDefaultConfig_ServerPort(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Port == 0 {
		t.Error("default server port should not be 0")
	}
}

func TestDefaultConfig_DatabasePath(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Database.Path == "" {
		t.Error("default database path should not be empty")
	}
}

func TestDefaultConfig_AgentName(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Agent.Name == "" {
		t.Error("default agent name should not be empty")
	}
}

func TestDefaultConfig_Memory(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Memory.WorkingBudget <= 0 {
		t.Error("working budget should be positive")
	}
}

func TestDefaultConfig_Cache(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Cache.TTLSeconds <= 0 {
		t.Error("cache TTL should be positive")
	}
}

func TestDefaultConfig_Treasury(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Treasury.PerPaymentCap <= 0 {
		t.Error("per-payment cap should be positive")
	}
}

func TestNewError_Basic(t *testing.T) {
	err := NewError(ErrConfig, "test error")
	if err == nil {
		t.Fatal("should not be nil")
	}
	if err.Error() == "" {
		t.Error("message should not be empty")
	}
}

func TestWrapError_Basic(t *testing.T) {
	inner := NewError(ErrDatabase, "inner")
	wrapped := WrapError(ErrLLM, "outer", inner)
	if wrapped == nil {
		t.Fatal("should not be nil")
	}
}

func TestAuthorityLevel_Strings(t *testing.T) {
	levels := []AuthorityLevel{AuthorityCreator, AuthorityExternal}
	for _, level := range levels {
		if level.String() == "" {
			t.Errorf("level %d has empty string", level)
		}
	}
}

func TestMaxUserMessageBytes_Positive(t *testing.T) {
	if MaxUserMessageBytes <= 0 {
		t.Error("should be positive")
	}
}

func TestDefaultServerPort_Positive(t *testing.T) {
	if DefaultServerPort <= 0 {
		t.Error("should be positive")
	}
}

func TestDefaultServerBind_NotEmpty(t *testing.T) {
	if DefaultServerBind == "" {
		t.Error("should not be empty")
	}
}
