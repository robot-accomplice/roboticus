package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestStore_InTx_Commit(t *testing.T) {
	store := openTestStore(t)

	err := store.InTx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO sessions (id, agent_id, scope_key) VALUES ('tx1', 'a1', 'test')`)
		return err
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}

	var count int
	row := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sessions WHERE id = 'tx1'`)
	_ = row.Scan(&count)
	if count != 1 {
		t.Errorf("count = %d after commit, want 1", count)
	}
}

func TestStore_InTx_Rollback(t *testing.T) {
	store := openTestStore(t)

	_ = store.InTx(context.Background(), func(tx *sql.Tx) error {
		_, _ = tx.Exec(`INSERT INTO sessions (id, agent_id, scope_key) VALUES ('tx2', 'a1', 'test')`)
		return context.Canceled
	})

	var count int
	row := store.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sessions WHERE id = 'tx2'`)
	_ = row.Scan(&count)
	if count != 0 {
		t.Errorf("count = %d after rollback, want 0", count)
	}
}

func TestStore_DB_Stats(t *testing.T) {
	store := openTestStore(t)
	db := store.DB()
	if db == nil {
		t.Fatal("DB() should not be nil")
	}
}

func TestNewID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewID()
		if ids[id] {
			t.Fatalf("duplicate ID: %s", id)
		}
		ids[id] = true
	}
}

func TestNewID_Length(t *testing.T) {
	id := NewID()
	if len(id) < 16 {
		t.Errorf("ID too short: %s (%d chars)", id, len(id))
	}
}

func TestHippocampusRegistry_Coverage(t *testing.T) {
	store := openTestStore(t)
	hippo := NewHippocampusRegistry(store)

	err := hippo.RegisterTable(context.Background(), "test_table", "a test", `[{"name":"col1","type":"TEXT"}]`)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	tables, err := hippo.ListTables(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, tbl := range tables {
		if tbl.Name == "test_table" {
			found = true
		}
	}
	if !found {
		t.Error("test_table not found in list")
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
