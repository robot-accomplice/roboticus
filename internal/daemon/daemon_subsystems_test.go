package daemon

import (
	"testing"
	"time"

	"roboticus/internal/core"
	"roboticus/internal/schedule"
)

func TestConsolidationHeartbeatInterval_PrefersMemoryInterval(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Heartbeat.IntervalSeconds = 300
	cfg.Heartbeat.MemoryIntervalSeconds = 90

	d := &Daemon{cfg: &cfg}
	if got := d.consolidationHeartbeatInterval(); got != 90*time.Second {
		t.Fatalf("interval = %v, want %v", got, 90*time.Second)
	}
}

func TestConsolidationHeartbeatInterval_FallsBackToGlobalHeartbeat(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Heartbeat.IntervalSeconds = 180
	cfg.Heartbeat.MemoryIntervalSeconds = 0

	d := &Daemon{cfg: &cfg}
	if got := d.consolidationHeartbeatInterval(); got != 180*time.Second {
		t.Fatalf("interval = %v, want %v", got, 180*time.Second)
	}
}

func TestConsolidationHeartbeatInterval_FallsBackToOneHour(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Heartbeat.IntervalSeconds = 0
	cfg.Heartbeat.MemoryIntervalSeconds = 0

	d := &Daemon{cfg: &cfg}
	if got := d.consolidationHeartbeatInterval(); got != time.Hour {
		t.Fatalf("interval = %v, want %v", got, time.Hour)
	}
}

func TestNewConsolidationHeartbeat_ConfiguresMemoryTaskAndIntervals(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Heartbeat.IntervalSeconds = 0
	cfg.Heartbeat.MemoryIntervalSeconds = 120

	d := &Daemon{cfg: &cfg}
	heartbeat, intervals, tickCtxFn := d.newConsolidationHeartbeat()
	if heartbeat == nil {
		t.Fatal("heartbeat should not be nil")
	}
	if len(heartbeat.Tasks()) != 1 {
		t.Fatalf("task count = %d, want 1", len(heartbeat.Tasks()))
	}
	if heartbeat.Tasks()[0].Kind() != schedule.TaskMemoryPrune {
		t.Fatalf("task kind = %q, want %q", heartbeat.Tasks()[0].Kind(), schedule.TaskMemoryPrune)
	}
	if intervals.Memory != 120*time.Second {
		t.Fatalf("memory interval = %v, want %v", intervals.Memory, 120*time.Second)
	}
	tctx := tickCtxFn()
	if tctx == nil {
		t.Fatal("tick context should not be nil")
	}
	if tctx.Timestamp.IsZero() {
		t.Fatal("tick context timestamp should be set")
	}
}

func TestMaintenanceHeartbeatEnabled_RequiresConfiguredInterval(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Heartbeat.IntervalSeconds = 0
	cfg.Heartbeat.MaintenanceIntervalSeconds = 0

	d := &Daemon{cfg: &cfg}
	if d.maintenanceHeartbeatEnabled() {
		t.Fatal("maintenance heartbeat should be disabled")
	}
}

func TestMaintenanceHeartbeatInterval_PrefersMaintenanceInterval(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Heartbeat.IntervalSeconds = 300
	cfg.Heartbeat.MaintenanceIntervalSeconds = 75

	d := &Daemon{cfg: &cfg}
	if got := d.maintenanceHeartbeatInterval(); got != 75*time.Second {
		t.Fatalf("interval = %v, want %v", got, 75*time.Second)
	}
}

func TestNewMaintenanceHeartbeat_ConfiguresMaintenanceTaskAndIntervals(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Database.Path = t.TempDir() + "/sched.db"
	cfg.Heartbeat.IntervalSeconds = 0
	cfg.Heartbeat.MaintenanceIntervalSeconds = 150

	d, err := New(&cfg, BootOptions{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = d.Stop(nil) }()

	heartbeat, intervals, tickCtxFn := d.newMaintenanceHeartbeat()
	if heartbeat == nil {
		t.Fatal("heartbeat should not be nil")
	}
	if len(heartbeat.Tasks()) != 1 {
		t.Fatalf("task count = %d, want 1", len(heartbeat.Tasks()))
	}
	if heartbeat.Tasks()[0].Kind() != schedule.TaskCacheEvict {
		t.Fatalf("task kind = %q, want %q", heartbeat.Tasks()[0].Kind(), schedule.TaskCacheEvict)
	}
	if intervals.Memory != 150*time.Second {
		t.Fatalf("maintenance interval = %v, want %v", intervals.Memory, 150*time.Second)
	}
	tctx := tickCtxFn()
	if tctx == nil || tctx.Timestamp.IsZero() {
		t.Fatal("tick context should be populated")
	}
}
