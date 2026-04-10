package db

import (
	"context"
	"strings"
	"testing"
)

func TestHippocampus_GetTable(t *testing.T) {
	store := testTempStore(t)
	reg := NewHippocampusRegistry(store)
	ctx := context.Background()

	// Register a table.
	cols := []ColumnDef{
		{Name: "id", ColType: "TEXT", Nullable: false},
		{Name: "value", ColType: "REAL", Nullable: true, Description: "The value"},
	}
	if err := reg.RegisterTableFull(ctx, "test_table", "A test table", cols, "system", false, "read", 42); err != nil {
		t.Fatalf("RegisterTableFull: %v", err)
	}

	// Get it back.
	entry, err := reg.GetTable(ctx, "test_table")
	if err != nil {
		t.Fatalf("GetTable: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if entry.Description != "A test table" {
		t.Errorf("description = %q", entry.Description)
	}
	if entry.AccessLevel != "read" {
		t.Errorf("access_level = %q, want read", entry.AccessLevel)
	}
	if entry.RowCount != 42 {
		t.Errorf("row_count = %d, want 42", entry.RowCount)
	}
	if len(entry.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(entry.Columns))
	}
	if entry.Columns[1].Description != "The value" {
		t.Errorf("col description = %q", entry.Columns[1].Description)
	}

	// Nonexistent returns nil.
	entry, err = reg.GetTable(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetTable nonexistent: %v", err)
	}
	if entry != nil {
		t.Error("expected nil for nonexistent")
	}
}

func TestHippocampus_ListAgentTables(t *testing.T) {
	store := testTempStore(t)
	reg := NewHippocampusRegistry(store)
	ctx := context.Background()

	// Register system + agent tables.
	reg.RegisterTableFull(ctx, "system_table", "System", nil, "system", false, "internal", 0)
	reg.RegisterTableFull(ctx, "agent1_data", "Agent data", nil, "agent1", true, "readwrite", 10)
	reg.RegisterTableFull(ctx, "agent2_data", "Other agent", nil, "agent2", true, "readwrite", 5)

	tables, err := reg.ListAgentTables(ctx, "agent1")
	if err != nil {
		t.Fatalf("ListAgentTables: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected 1, got %d", len(tables))
	}
	if tables[0].TableName != "agent1_data" {
		t.Errorf("got %q", tables[0].TableName)
	}
}

func TestHippocampus_CreateAndDropAgentTable(t *testing.T) {
	store := testTempStore(t)
	reg := NewHippocampusRegistry(store)
	ctx := context.Background()

	cols := []ColumnDef{
		{Name: "value", ColType: "TEXT", Nullable: false},
		{Name: "score", ColType: "REAL", Nullable: true},
	}

	// Create.
	name, err := reg.CreateAgentTable(ctx, "bot1", "metrics", "Bot metrics", cols)
	if err != nil {
		t.Fatalf("CreateAgentTable: %v", err)
	}
	if name != "bot1_metrics" {
		t.Errorf("name = %q", name)
	}

	// Verify it's registered.
	entry, _ := reg.GetTable(ctx, "bot1_metrics")
	if entry == nil {
		t.Fatal("expected hippocampus entry")
	}
	if !entry.AgentOwned {
		t.Error("expected agent_owned = true")
	}

	// Insert data into the created table.
	_, err = store.ExecContext(ctx,
		`INSERT INTO "bot1_metrics" (id, value, score) VALUES ('r1', 'test', 3.14)`)
	if err != nil {
		t.Fatalf("insert into agent table: %v", err)
	}

	// Drop it.
	if err := reg.DropAgentTable(ctx, store, "bot1", "bot1_metrics"); err != nil {
		t.Fatalf("DropAgentTable: %v", err)
	}

	// Should be gone from hippocampus.
	entry, _ = reg.GetTable(ctx, "bot1_metrics")
	if entry != nil {
		t.Error("expected nil after drop")
	}
}

func TestHippocampus_DropDeniedForNonOwner(t *testing.T) {
	store := testTempStore(t)
	reg := NewHippocampusRegistry(store)
	ctx := context.Background()

	reg.CreateAgentTable(ctx, "bot1", "private", "Private data", nil)

	// Different agent tries to drop.
	err := reg.DropAgentTable(ctx, store, "bot2", "bot1_private")
	if err == nil {
		t.Fatal("expected error for non-owner drop")
	}
}

func TestHippocampus_SchemaSummary(t *testing.T) {
	store := testTempStore(t)
	reg := NewHippocampusRegistry(store)
	ctx := context.Background()

	cols := []ColumnDef{
		{Name: "id", ColType: "TEXT", Nullable: false, Description: "Primary key"},
	}
	reg.RegisterTableFull(ctx, "test_t", "Test table", cols, "system", false, "read", 100)

	summary, err := reg.SchemaSummary(ctx)
	if err != nil {
		t.Fatalf("SchemaSummary: %v", err)
	}
	if !strings.Contains(summary, "test_t") {
		t.Error("missing table name in summary")
	}
	if !strings.Contains(summary, "100 rows") {
		t.Error("missing row count")
	}
	if !strings.Contains(summary, "Primary key") {
		t.Error("missing column description")
	}
}

func TestHippocampus_CompactSummary(t *testing.T) {
	store := testTempStore(t)
	reg := NewHippocampusRegistry(store)
	ctx := context.Background()

	reg.RegisterTableFull(ctx, "sessions", "User sessions", nil, "system", false, "read", 50)
	reg.RegisterTableFull(ctx, "bot1_data", "Bot data", nil, "bot1", true, "readwrite", 10)

	summary, err := reg.CompactSummary(ctx)
	if err != nil {
		t.Fatalf("CompactSummary: %v", err)
	}
	if !strings.Contains(summary, "[Database]") {
		t.Error("missing [Database] header")
	}
	if !strings.Contains(summary, "Your tables:") {
		t.Error("missing agent tables section")
	}
	if !strings.Contains(summary, "bot1_data") {
		t.Error("missing agent table name")
	}
	if len(summary) > 1100 {
		t.Errorf("summary too long: %d chars", len(summary))
	}
}

func TestHippocampus_Bootstrap(t *testing.T) {
	store := testTempStore(t)
	reg := NewHippocampusRegistry(store)
	ctx := context.Background()

	if err := reg.BootstrapHippocampus(ctx); err != nil {
		t.Fatalf("BootstrapHippocampus: %v", err)
	}

	// Should have discovered system tables.
	tables, err := reg.ListAllTables(ctx)
	if err != nil {
		t.Fatalf("ListAllTables: %v", err)
	}
	if len(tables) == 0 {
		t.Error("expected discovered tables")
	}

	// sessions should be registered with known metadata.
	sessEntry, _ := reg.GetTable(ctx, "sessions")
	if sessEntry == nil {
		t.Fatal("expected sessions in hippocampus")
	}
	if sessEntry.AccessLevel != "read" {
		t.Errorf("sessions access_level = %q, want read", sessEntry.AccessLevel)
	}
}

func TestValidateSQLIdentifier(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"good_name", true},
		{"CamelCase", true},
		{"abc123", true},
		{"", false},
		{"123start", false},
		{"has-dash", false},
		{"has space", false},
		{"has.dot", false},
	}
	for _, tt := range tests {
		err := validateSQLIdentifier(tt.input)
		if (err == nil) != tt.valid {
			t.Errorf("validateSQLIdentifier(%q) = %v, want valid=%v", tt.input, err, tt.valid)
		}
	}
}
