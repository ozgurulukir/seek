package parserdef

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// buildHermesFixture creates a state.db matching Hermes Agent's canonical
// sessions/messages schema (hermes_state_common.py DDL subset) with realistic
// rows: two profiles' worth of sessions, user/assistant/tool roles, a
// compressed-summary row, and an inactive row.
func buildHermesFixture(t *testing.T, dir string) {
	t.Helper()
	dbPath := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ddl := `
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    session_key TEXT,
    chat_id TEXT,
    chat_type TEXT,
    thread_id TEXT,
    display_name TEXT,
    model TEXT,
    title TEXT,
    started_at REAL NOT NULL,
    ended_at REAL,
    last_activity_at REAL,
    profile_name TEXT,
    active INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE messages (
    id INTEGER PRIMARY KEY,
    session_id TEXT,
    role TEXT,
    content TEXT,
    tool_calls TEXT,
    timestamp REAL NOT NULL,
    _compressed_summary INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 1,
    compacted INTEGER NOT NULL DEFAULT 0
);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("fixture DDL: %v", err)
	}

	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC).Unix()
	ins := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("fixture exec: %v", err)
		}
	}
	ins(`INSERT INTO sessions (id, source, display_name, model, started_at, ended_at, last_activity_at, profile_name)
		VALUES ('s-cli', 'cli', 'CLI session', 'qwen3-max', ?, ?, ?, 'default')`,
		float64(base), float64(base+60), float64(base+120))
	ins(`INSERT INTO sessions (id, source, display_name, model, started_at, ended_at, last_activity_at, profile_name)
		VALUES ('s-tg', 'telegram', '+1555 DM', 'qwen3-max', ?, ?, ?, 'main')`,
		float64(base+300), float64(base+400), float64(base+500))
	ins(`INSERT INTO sessions (id, source, display_name, model, started_at, ended_at, last_activity_at, profile_name)
		VALUES ('s-inact', 'cli', 'Inactive', 'qwen3-max', ?, NULL, NULL, 'default')`,
		float64(base+700))
	db.Exec(`UPDATE sessions SET active = 0 WHERE id = 's-inact'`)

	ins(`INSERT INTO messages (session_id, role, content, timestamp) VALUES
		('s-cli', 'user',      'reboot the gateway',  ?),
		('s-cli', 'assistant', 'rebooting now.',      ?),
		('s-tg',  'user',      'status?',             ?)`,
		float64(base+10), float64(base+20), float64(base+310))
	// tool message + compressed + inactive + whitespace-only rows that must be skipped
	ins(`INSERT INTO messages (session_id, role, content, timestamp) VALUES
		('s-cli', 'tool', '{"exit":0}', ?)`, float64(base+25))
	ins(`INSERT INTO messages (session_id, role, content, timestamp, _compressed_summary) VALUES
		('s-cli', 'assistant', 'summary blob', ?, 1)`, float64(base+30))
	ins(`INSERT INTO messages (session_id, role, content, timestamp, active) VALUES
		('s-cli', 'user', 'soft-deleted', ?, 0)`, float64(base+35))
	ins(`INSERT INTO messages (session_id, role, content, timestamp, compacted) VALUES
		('s-cli', 'user', 'compacted away', ?, 1)`, float64(base+40))
	ins(`INSERT INTO messages (session_id, role, content, timestamp) VALUES
		('s-cli', 'user', '   ', ?)`, float64(base+45))
}

func TestHermesSchema_RootDB(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	hermesDir := filepath.Join(tmpHome, ".hermes")
	if err := os.MkdirAll(hermesDir, 0700); err != nil {
		t.Fatal(err)
	}
	buildHermesFixture(t, hermesDir)

	def, err := Load("hermes")
	if err != nil {
		t.Fatalf("load hermes schema: %v", err)
	}
	src, ver, files, err := detectSQLiteSource(def)
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if src == nil || len(files) == 0 {
		t.Fatal("hermes source did not match fixture")
	}
	if len(files) != 1 {
		t.Fatalf("root source matched %d files, want 1 (state.db)", len(files))
	}

	sessions, errs, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected session errors: %v", errs)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions (%v), want 2 (inactive excluded)", len(sessions), sessions)
	}

	byID := map[string]Session{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	cli := byID["s-cli"]
	// tool/JSON messages are kept (tool-call history is real content); the
	// skipped classes are compressed summaries, soft-deleted, compacted, and
	// whitespace-only rows.
	if len(cli.Messages) != 3 {
		t.Fatalf("s-cli messages = %d (%v), want 3", len(cli.Messages), cli.Messages)
	}
	if cli.Messages[0].Role != "user" || cli.Messages[0].Content != "reboot the gateway" {
		t.Errorf("first message = %+v, want user/reboot", cli.Messages[0])
	}
	if cli.Messages[1].Role != "assistant" {
		t.Errorf("second message role = %q, want assistant", cli.Messages[1].Role)
	}
	if cli.Metadata["platform"] != "cli" || cli.Metadata["profile"] != "default" {
		t.Errorf("metadata = %v, want platform=cli profile=default", cli.Metadata)
	}
	if cli.Cursor.IsZero() {
		t.Error("s-cli cursor is zero")
	}
	if want := "CLI session"; cli.Title != want {
		t.Errorf("title = %q, want %q", cli.Title, want)
	}
	tg := byID["s-tg"]
	if tg.Metadata["platform"] != "telegram" || tg.Metadata["profile"] != "main" {
		t.Errorf("tg metadata = %v, want platform=telegram profile=main", tg.Metadata)
	}
	if len(tg.Messages) != 1 {
		t.Fatalf("s-tg messages = %d, want 1", len(tg.Messages))
	}
}

func TestHermesSchema_ProfilesDBs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	profilesDir := filepath.Join(tmpHome, ".hermes", "profiles")
	for _, name := range []string{"work", "personal"} {
		dir := filepath.Join(profilesDir, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		buildHermesFixture(t, dir)
	}

	def, err := Load("hermes")
	if err != nil {
		t.Fatalf("load hermes schema: %v", err)
	}
	// Source #2 (profiles glob) must resolve: root state.db absent here.
	src, ver, files, err := detectSQLiteSource(def)
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("matched %d profile DBs, want 2", len(files))
	}

	sessions, errs, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected session errors: %v", errs)
	}
	// Same session IDs exist in both profiles; dedup is indexer-level, engine
	// reports both occurrences.
	if len(sessions) != 4 {
		t.Fatalf("got %d sessions, want 4 (2 per profile db)", len(sessions))
	}
}

func TestHermesSchema_IncrementalSkip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	hermesDir := filepath.Join(tmpHome, ".hermes")
	if err := os.MkdirAll(hermesDir, 0700); err != nil {
		t.Fatal(err)
	}
	buildHermesFixture(t, hermesDir)

	def, _ := Load("hermes")
	src, ver, files, err := detectSQLiteSource(def)
	if err != nil {
		t.Fatal(err)
	}

	// "since = now" → no session cursor is after it → all unchanged (Messages nil).
	sessions, _, err := SyncSessions(src, ver, files, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s.Messages != nil {
			t.Errorf("session %s re-fetched messages despite incremental skip", s.ID)
		}
	}
}

func TestHermesSchema_DetectionNegative(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// A state.db WITHOUT the hermes tables must not match the version rules.
	hermesDir := filepath.Join(tmpHome, ".hermes")
	if err := os.MkdirAll(hermesDir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(hermesDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE other (x TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	def, _ := Load("hermes")
	_, _, files, err := detectSQLiteSource(def)
	if err == nil {
		t.Errorf("expected resolveSource to reject non-hermes state.db (matched files: %v)", files)
	}
}
