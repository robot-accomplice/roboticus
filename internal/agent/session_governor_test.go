package agent_test

import (
	"context"
	"fmt"
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

func TestSessionGovernor_CompactBeforeArchive(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Create a session.
	_, err := store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key, status)
		 VALUES ('compact-sess', 'agent1', 'test', 'active')`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Insert 10 messages.
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		_, err := store.ExecContext(ctx,
			`INSERT INTO session_messages (id, session_id, role, content, created_at)
			 VALUES (?, 'compact-sess', ?, ?, datetime('now', ?))`,
			fmt.Sprintf("msg-%d", i), role, fmt.Sprintf("message %d content", i),
			fmt.Sprintf("-%d minutes", 60-i))
		if err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
	}

	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)
	err = sg.CompactBeforeArchive(ctx, "compact-sess")
	if err != nil {
		t.Fatalf("compact error: %v", err)
	}

	// Count remaining messages — should have summary + preserved recent messages.
	var count int
	err = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_messages WHERE session_id = 'compact-sess'`).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	// 10 messages: oldest 50% = 5 compacted, 5 preserved + 1 summary = 6.
	// But preserve min is 4, so halfPoint = min(5, 10-4=6) = 5.
	// Result: 5 deleted, 1 summary inserted, 5 kept = 6 total.
	if count != 6 {
		t.Errorf("expected 6 messages after compaction, got %d", count)
	}

	// Verify summary message exists.
	var summaryContent string
	err = store.QueryRowContext(ctx,
		`SELECT content FROM session_messages WHERE session_id = 'compact-sess' AND role = 'system'`).Scan(&summaryContent)
	if err != nil {
		t.Fatalf("summary query: %v", err)
	}
	if len(summaryContent) == 0 {
		t.Error("expected non-empty summary content")
	}
}

func TestSessionGovernor_CompactBeforeArchive_FewMessages(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	_, err := store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, scope_key, status)
		 VALUES ('few-sess', 'agent1', 'test', 'active')`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Insert only 3 messages — fewer than preserveRecent (4).
	for i := 0; i < 3; i++ {
		_, err := store.ExecContext(ctx,
			`INSERT INTO session_messages (id, session_id, role, content, created_at)
			 VALUES (?, 'few-sess', 'user', ?, datetime('now'))`,
			fmt.Sprintf("fmsg-%d", i), fmt.Sprintf("msg %d", i))
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	sg := agent.NewSessionGovernor(store, 24*time.Hour, 24*time.Hour)
	err = sg.CompactBeforeArchive(ctx, "few-sess")
	if err != nil {
		t.Fatalf("compact error: %v", err)
	}

	// All 3 messages should remain — nothing to compact.
	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_messages WHERE session_id = 'few-sess'`).Scan(&count)
	if count != 3 {
		t.Errorf("expected 3 messages (no compaction), got %d", count)
	}
}

func TestAdjustPriority(t *testing.T) {
	tests := []struct {
		name     string
		success  int
		failure  int
		boost    int
		decay    int
		expected int
	}{
		{"high success rate boosts", 9, 1, 5, 10, 5},
		{"exactly 80% boosts", 8, 2, 5, 10, 5},
		{"high failure decays", 2, 8, 5, 10, -10},
		{"balanced returns 0", 5, 5, 5, 10, 0}, // 50% failure rate == 0.5, not > 0.5
		{"no calls returns 0", 0, 0, 5, 10, 0},
		{"default boost/decay", 9, 1, 0, 0, 5},
		{"all success boosts", 10, 0, 7, 15, 7},
		{"all failure decays", 0, 10, 5, 12, -12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agent.AdjustPriority(tt.success, tt.failure, tt.boost, tt.decay)
			if got != tt.expected {
				t.Errorf("AdjustPriority(%d, %d, %d, %d) = %d, want %d",
					tt.success, tt.failure, tt.boost, tt.decay, got, tt.expected)
			}
		})
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
