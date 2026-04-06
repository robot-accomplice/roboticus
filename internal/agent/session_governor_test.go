package agent_test

import (
	"context"
	"testing"
	"time"

	"roboticus/internal/agent"
	"roboticus/testutil"
)

func TestSessionGovernor_NewSessionGovernor(t *testing.T) {
	store := testutil.TempStore(t)
	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)
	if sg == nil {
		t.Fatal("expected non-nil SessionGovernor")
	}
}

func TestSessionGovernor_TickEmpty(t *testing.T) {
	store := testutil.TempStore(t)
	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)

	report, err := sg.Tick(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.ExpiredSessions != 0 {
		t.Errorf("expected 0 expired sessions, got %d", report.ExpiredSessions)
	}
	if report.DecayedMemories != 0 {
		t.Errorf("expected 0 decayed memories, got %d", report.DecayedMemories)
	}
	if report.AdjustedSkills != 0 {
		t.Errorf("expected 0 adjusted skills, got %d", report.AdjustedSkills)
	}
	if report.PrunedSkills != 0 {
		t.Errorf("expected 0 pruned skills, got %d", report.PrunedSkills)
	}
}

func TestSessionGovernor_ExpireStaleSessions(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Insert an active session with created_at far in the past.
	_, err := store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key, status, created_at)
		 VALUES ('old-session', 'agent1', 'test', 'active', datetime('now', '-48 hours'))`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Insert a recent active session that should not be expired.
	_, err = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key, status, created_at)
		 VALUES ('new-session', 'agent1', 'test2', 'active', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)
	report, err := sg.Tick(ctx)
	if err != nil {
		t.Fatalf("tick error: %v", err)
	}
	if report.ExpiredSessions != 1 {
		t.Errorf("expected 1 expired session, got %d", report.ExpiredSessions)
	}

	// Verify the old session is expired.
	row := store.QueryRowContext(ctx, `SELECT status FROM sessions WHERE id = 'old-session'`)
	var status string
	if err := row.Scan(&status); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "expired" {
		t.Errorf("expected expired, got %s", status)
	}

	// Verify the new session is still active.
	row = store.QueryRowContext(ctx, `SELECT status FROM sessions WHERE id = 'new-session'`)
	if err := row.Scan(&status); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "active" {
		t.Errorf("expected active, got %s", status)
	}
}

func TestSessionGovernor_DecayEpisodicImportance(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Insert an old episodic memory with high importance.
	_, err := store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, created_at)
		 VALUES ('old-mem', 'observation', 'test content', 8, datetime('now', '-10 days'))`)
	if err != nil {
		t.Fatalf("insert memory: %v", err)
	}

	// Insert a recent memory that should not decay.
	_, err = store.ExecContext(ctx,
		`INSERT INTO episodic_memory (id, classification, content, importance, created_at)
		 VALUES ('new-mem', 'observation', 'recent content', 8, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert memory: %v", err)
	}

	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)
	report, err := sg.Tick(ctx)
	if err != nil {
		t.Fatalf("tick error: %v", err)
	}
	if report.DecayedMemories != 1 {
		t.Errorf("expected 1 decayed memory, got %d", report.DecayedMemories)
	}

	// Verify old memory importance decreased.
	row := store.QueryRowContext(ctx, `SELECT importance FROM episodic_memory WHERE id = 'old-mem'`)
	var importance int
	if err := row.Scan(&importance); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if importance != 7 {
		t.Errorf("expected importance 7, got %d", importance)
	}
}

func TestSessionGovernor_AdjustSkillPriorities(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Insert a high-failure skill (failure_count > success_count * 2).
	_, err := store.ExecContext(ctx,
		`INSERT INTO learned_skills (id, name, success_count, failure_count, priority)
		 VALUES ('bad-skill', 'failing_skill', 2, 10, 50)`)
	if err != nil {
		t.Fatalf("insert skill: %v", err)
	}

	// Insert a high-success skill (success_count > 10, failure_count == 0).
	_, err = store.ExecContext(ctx,
		`INSERT INTO learned_skills (id, name, success_count, failure_count, priority)
		 VALUES ('good-skill', 'great_skill', 15, 0, 80)`)
	if err != nil {
		t.Fatalf("insert skill: %v", err)
	}

	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)
	report, err := sg.Tick(ctx)
	if err != nil {
		t.Fatalf("tick error: %v", err)
	}
	if report.AdjustedSkills != 2 {
		t.Errorf("expected 2 adjusted skills, got %d", report.AdjustedSkills)
	}

	// Verify bad skill priority decreased (50 * 90 / 100 = 45).
	row := store.QueryRowContext(ctx, `SELECT priority FROM learned_skills WHERE id = 'bad-skill'`)
	var priority int
	if err := row.Scan(&priority); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if priority != 45 {
		t.Errorf("expected priority 45, got %d", priority)
	}

	// Verify good skill priority increased (80 * 105 / 100 = 84).
	row = store.QueryRowContext(ctx, `SELECT priority FROM learned_skills WHERE id = 'good-skill'`)
	if err := row.Scan(&priority); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if priority != 84 {
		t.Errorf("expected priority 84, got %d", priority)
	}
}

func TestSessionGovernor_PruneDeadSkills(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Insert a very low priority skill that should be pruned.
	_, err := store.ExecContext(ctx,
		`INSERT INTO learned_skills (id, name, priority, memory_state)
		 VALUES ('dead-skill', 'useless_skill', 3, 'active')`)
	if err != nil {
		t.Fatalf("insert skill: %v", err)
	}

	// Insert a normal priority skill that should not be pruned.
	_, err = store.ExecContext(ctx,
		`INSERT INTO learned_skills (id, name, priority, memory_state)
		 VALUES ('alive-skill', 'good_skill', 50, 'active')`)
	if err != nil {
		t.Fatalf("insert skill: %v", err)
	}

	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)
	report, err := sg.Tick(ctx)
	if err != nil {
		t.Fatalf("tick error: %v", err)
	}
	if report.PrunedSkills != 1 {
		t.Errorf("expected 1 pruned skill, got %d", report.PrunedSkills)
	}

	// Verify dead skill is pruned.
	row := store.QueryRowContext(ctx, `SELECT memory_state FROM learned_skills WHERE id = 'dead-skill'`)
	var state string
	if err := row.Scan(&state); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if state != "pruned" {
		t.Errorf("expected pruned, got %s", state)
	}

	// Verify alive skill is still active.
	row = store.QueryRowContext(ctx, `SELECT memory_state FROM learned_skills WHERE id = 'alive-skill'`)
	if err := row.Scan(&state); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if state != "active" {
		t.Errorf("expected active, got %s", state)
	}
}

func TestSessionGovernor_MinInterval(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)

	// First tick should run.
	report1, err := sg.Tick(ctx)
	if err != nil {
		t.Fatalf("first tick error: %v", err)
	}
	_ = report1

	// Immediate second tick should be a no-op (within minInterval).
	report2, err := sg.Tick(ctx)
	if err != nil {
		t.Fatalf("second tick error: %v", err)
	}
	// The second report should have all zeros since it was skipped.
	if report2.ExpiredSessions != 0 || report2.DecayedMemories != 0 ||
		report2.AdjustedSkills != 0 || report2.PrunedSkills != 0 {
		t.Errorf("expected empty report for skipped tick, got %+v", report2)
	}
}
