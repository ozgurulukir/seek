package parserdef

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/internal/source"
)

// ---- Copilot CLI (Mode A) fixture tests ----

// createCopilotFixtureDB creates an external SQLite DB mimicking the Copilot
// CLI session-store.db schema (sessions + turns tables).
func createCopilotFixtureDB(t *testing.T, path string) {
	t.Helper()
	osRemoveAll(path, path+"-wal", path+"-shm")

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open copilot fixture db: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			cwd TEXT,
			summary TEXT,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE turns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			turn_index INTEGER NOT NULL,
			user_message TEXT,
			assistant_response TEXT,
			timestamp TEXT
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}

	// Session 1: two turns (user + assistant each).
	// RFC3339 cursor with millisecond precision + Z suffix (matches real data).
	t1 := "2026-06-07T13:13:03.648Z"
	t2 := "2026-06-08T09:00:00.000Z"

	db.Exec(`INSERT INTO sessions (id, cwd, summary, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"copilot-sess-1", "/home/user/myproject", "Fix bug in parser", t1, t1)
	db.Exec(`INSERT INTO turns (session_id, turn_index, user_message, assistant_response) VALUES (?, ?, ?, ?)`,
		"copilot-sess-1", 0, "What does this error mean?", "It means the input is malformed.")
	db.Exec(`INSERT INTO turns (session_id, turn_index, user_message, assistant_response) VALUES (?, ?, ?, ?)`,
		"copilot-sess-1", 1, "How do I fix it?", "Add a null check before parsing.")

	// Session 2: no cwd (empty workspace metadata is acceptable).
	db.Exec(`INSERT INTO sessions (id, cwd, summary, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"copilot-sess-2", "", "Quick chat", t2, t2)
	db.Exec(`INSERT INTO turns (session_id, turn_index, user_message, assistant_response) VALUES (?, ?, ?, ?)`,
		"copilot-sess-2", 0, "Hello", "Hi there!")
}

func makeCopilotDef(dbPath string) *ParserDef {
	return &ParserDef{
		Format: 1,
		Name:   "test-copilot",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{dbPath},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					Query:        `SELECT id, summary, cwd, updated_at FROM sessions`,
					ID:           "id",
					Title:        "summary",
					Cursor:       "updated_at",
					CursorFormat: "rfc3339",
					Metadata:     map[string]string{"workspace": "cwd"},
				},
				Messages: MessagesSpec{
					Query: `SELECT turn_index, 1 AS half, 'user' AS role, user_message AS content
						FROM turns WHERE session_id = :session_id
						UNION ALL
						SELECT turn_index, 2, 'assistant', assistant_response
						FROM turns WHERE session_id = :session_id
						ORDER BY turn_index, half`,
					Role:    "role",
					Content: "content",
				},
			}},
		}},
	}
}

func TestCopilotFixture_FullSync(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "copilot.db")
	createCopilotFixtureDB(t, dbPath)

	def := makeCopilotDef(dbPath)
	src, ver, files, err := def.Match()
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}

	sessions, sErrs, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(sErrs) != 0 {
		t.Fatalf("session errors: %v", sErrs)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}

	// Find sess-1.
	var s1 *Session
	for i := range sessions {
		if sessions[i].ID == "copilot-sess-1" {
			s1 = &sessions[i]
		}
	}
	if s1 == nil {
		t.Fatal("copilot-sess-1 not found")
	}

	// RFC3339 cursor must parse with millisecond precision.
	want := time.Date(2026, 6, 7, 13, 13, 3, 648000000, time.UTC)
	if !s1.Cursor.Equal(want) {
		t.Errorf("cursor = %v, want %v", s1.Cursor, want)
	}

	// Workspace metadata from cwd.
	if s1.Metadata["workspace"] != "/home/user/myproject" {
		t.Errorf("workspace = %q", s1.Metadata["workspace"])
	}

	// 4 messages: user, assistant, user, assistant — in turn order.
	// This is the critical interleaving test (ORDER BY turn_index, half).
	if len(s1.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(s1.Messages))
	}
	expected := []struct{ role, content string }{
		{source.RoleUser, "What does this error mean?"},
		{source.RoleAssistant, "It means the input is malformed."},
		{source.RoleUser, "How do I fix it?"},
		{source.RoleAssistant, "Add a null check before parsing."},
	}
	for i, e := range expected {
		if s1.Messages[i].Role != e.role {
			t.Errorf("msg[%d] role = %q, want %q", i, s1.Messages[i].Role, e.role)
		}
		if s1.Messages[i].Content != e.content {
			t.Errorf("msg[%d] content = %q, want %q", i, s1.Messages[i].Content, e.content)
		}
	}
}

// TestCopilotFixture_EmptyWorkspace verifies a session with empty cwd still works.
func TestCopilotFixture_EmptyWorkspace(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "copilot.db")
	createCopilotFixtureDB(t, dbPath)

	def := makeCopilotDef(dbPath)
	src, ver, files, _ := def.Match()

	sessions, _, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}

	var s2 *Session
	for i := range sessions {
		if sessions[i].ID == "copilot-sess-2" {
			s2 = &sessions[i]
		}
	}
	if s2 == nil {
		t.Fatal("copilot-sess-2 not found")
	}
	// Empty cwd is acceptable — just no workspace metadata value.
	if s2.Metadata["workspace"] != "" {
		t.Errorf("workspace = %q, want empty", s2.Metadata["workspace"])
	}
	if len(s2.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(s2.Messages))
	}
}

// ---- Zed (Mode B) fixture tests ----

// createZedFixtureDB creates an external SQLite DB mimicking the Zed
// threads.db schema, with zstd-compressed JSON blobs containing
// User/Agent message variants (Text, Thinking.text, ToolUse).
func createZedFixtureDB(t *testing.T, path string) {
	t.Helper()
	osRemoveAll(path, path+"-wal", path+"-shm")

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open zed fixture db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		summary TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		data_type TEXT NOT NULL,
		data BLOB NOT NULL,
		parent_id TEXT,
		folder_paths TEXT
	)`); err != nil {
		t.Fatalf("create threads table: %v", err)
	}

	// Build the conversation JSON blob and compress with zstd.
	thread1 := map[string]interface{}{
		"title": "Test Thread One",
		"messages": []interface{}{
			map[string]interface{}{
				"User": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{"Text": "Please fix the YAML error"},
					},
				},
			},
			map[string]interface{}{
				"Agent": map[string]interface{}{
					"content": []interface{}{
						// Thinking block — nested dot-path "Thinking.text".
						map[string]interface{}{"Thinking": map[string]interface{}{"text": "I should look at the frontmatter first."}},
						// Normal text response.
						map[string]interface{}{"Text": "The issue is a missing colon in line 3."},
						// ToolUse — should be EXCLUDED (not in text_keys).
						map[string]interface{}{"ToolUse": map[string]interface{}{"name": "read_file", "raw_input": "path/to/file"}},
					},
				},
			},
		},
	}

	thread2 := map[string]interface{}{
		"title": "Test Thread Two",
		"messages": []interface{}{
			map[string]interface{}{
				"User": map[string]interface{}{
					"content": []interface{}{
						// User with Mention — only Text should be extracted.
						map[string]interface{}{"Text": "Check this file"},
						map[string]interface{}{"Mention": map[string]interface{}{"content": "file reference content"}},
					},
				},
			},
			map[string]interface{}{
				"Agent": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{"Text": "All good."},
					},
				},
			},
		},
	}

	t1 := "2026-07-29T22:26:24.023418304+00:00"
	t2 := "2026-07-29T22:28:34.503829599+00:00"

	insertThread(t, db, threadInsertParams{
		id:      "zed-thread-1",
		summary: "Test Thread One",
		updated: t1,
		folder:  "/home/user/project-a",
		parent:  "",
		obj:     thread1,
	})
	insertThread(t, db, threadInsertParams{
		id:      "zed-thread-2",
		summary: "Test Thread Two",
		updated: t2,
		folder:  "/home/user/project-b",
		parent:  "zed-thread-1",
		obj:     thread2,
	})
}

type threadInsertParams struct {
	id      string
	summary string
	updated string
	folder  string
	parent  string
	obj     map[string]interface{}
}

func insertThread(t *testing.T, db *sql.DB, p threadInsertParams) {
	t.Helper()
	jsonBytes, err := json.Marshal(p.obj)
	if err != nil {
		t.Fatalf("marshal thread json: %v", err)
	}
	// Compress with zstd.
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("create zstd encoder: %v", err)
	}
	compressed := enc.EncodeAll(jsonBytes, nil)
	enc.Close()

	_, err = db.Exec(`INSERT INTO threads (id, summary, updated_at, data_type, data, parent_id, folder_paths) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.id, p.summary, p.updated, "zstd", compressed, p.parent, p.folder)
	if err != nil {
		t.Fatalf("insert thread %s: %v", p.id, err)
	}
}

func makeZedDef(dbPath string) *ParserDef {
	return &ParserDef{
		Format: 1,
		Name:   "test-zed",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{dbPath},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					Query:        `SELECT id, summary, updated_at, data, folder_paths, parent_id FROM threads`,
					ID:           "id",
					Title:        "summary",
					Cursor:       "updated_at",
					CursorFormat: "rfc3339",
					Data:         "data",
					Metadata:     map[string]string{"workspace": "folder_paths", "parent": "parent_id"},
				},
				Messages: MessagesSpec{
					Inline: true,
					Decode: "zstd",
					Items:  "messages",
					Variants: []VariantSpec{
						{MatchKey: "User", Role: "user", ArrayField: "content", TextKeys: []string{"Text"}},
						{MatchKey: "Agent", Role: "assistant", ArrayField: "content", TextKeys: []string{"Text", "Thinking.text"}},
					},
				},
			}},
		}},
	}
}

func TestZedFixture_FullSync(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zed.db")
	createZedFixtureDB(t, dbPath)

	def := makeZedDef(dbPath)
	src, ver, files, err := def.Match()
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}

	sessions, sErrs, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(sErrs) != 0 {
		t.Fatalf("session errors: %v", sErrs)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}

	// Find thread-1.
	var s1 *Session
	for i := range sessions {
		if sessions[i].ID == "zed-thread-1" {
			s1 = &sessions[i]
		}
	}
	if s1 == nil {
		t.Fatal("zed-thread-1 not found")
	}

	// RFC3339 cursor with nanosecond precision.
	want := time.Date(2026, 7, 29, 22, 26, 24, 23418304, time.UTC)
	if !s1.Cursor.Equal(want) {
		t.Errorf("cursor = %v, want %v", s1.Cursor, want)
	}

	// Metadata.
	if s1.Metadata["workspace"] != "/home/user/project-a" {
		t.Errorf("workspace = %q", s1.Metadata["workspace"])
	}

	// 2 messages: User (Text only) + Agent (Thinking.text + Text, ToolUse excluded).
	if len(s1.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(s1.Messages))
	}

	// User message.
	if s1.Messages[0].Role != source.RoleUser {
		t.Errorf("msg[0] role = %q, want user", s1.Messages[0].Role)
	}
	if s1.Messages[0].Content != "Please fix the YAML error" {
		t.Errorf("msg[0] content = %q", s1.Messages[0].Content)
	}

	// Agent message: Thinking.text + Text concatenated, ToolUse excluded.
	if s1.Messages[1].Role != source.RoleAssistant {
		t.Errorf("msg[1] role = %q, want assistant", s1.Messages[1].Role)
	}
	if !contains(s1.Messages[1].Content, "I should look at the frontmatter first.") {
		t.Errorf("msg[1] missing Thinking.text: %q", s1.Messages[1].Content)
	}
	if !contains(s1.Messages[1].Content, "The issue is a missing colon in line 3.") {
		t.Errorf("msg[1] missing Text: %q", s1.Messages[1].Content)
	}
	if contains(s1.Messages[1].Content, "read_file") {
		t.Errorf("msg[1] should exclude ToolUse.raw_input: %q", s1.Messages[1].Content)
	}
}

// TestZedFixture_MentionExcluded verifies that Mention content is not extracted
// when only "Text" is in text_keys (matches the acceptance criterion scope).
func TestZedFixture_MentionExcluded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zed.db")
	createZedFixtureDB(t, dbPath)

	def := makeZedDef(dbPath)
	src, ver, files, _ := def.Match()

	sessions, _, _ := SyncSessions(src, ver, files, time.Time{})

	var s2 *Session
	for i := range sessions {
		if sessions[i].ID == "zed-thread-2" {
			s2 = &sessions[i]
		}
	}
	if s2 == nil {
		t.Fatal("zed-thread-2 not found")
	}

	// Parent metadata.
	if s2.Metadata["parent"] != "zed-thread-1" {
		t.Errorf("parent = %q, want zed-thread-1", s2.Metadata["parent"])
	}

	// User message should have only Text, not Mention content.
	if s2.Messages[0].Role != source.RoleUser {
		t.Errorf("msg[0] role = %q", s2.Messages[0].Role)
	}
	if s2.Messages[0].Content != "Check this file" {
		t.Errorf("msg[0] content = %q, want 'Check this file' (Mention excluded)", s2.Messages[0].Content)
	}
	if contains(s2.Messages[0].Content, "file reference content") {
		t.Errorf("msg[0] should exclude Mention.content: %q", s2.Messages[0].Content)
	}
}

// TestZedFixture_IncrementalSync verifies zstd sessions with old cursors are unchanged.
func TestZedFixture_IncrementalSync(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "zed.db")
	createZedFixtureDB(t, dbPath)

	def := makeZedDef(dbPath)
	src, ver, files, _ := def.Match()

	// Full sync.
	all, _, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("full sync: %v", err)
	}
	for _, s := range all {
		if s.Messages == nil {
			t.Errorf("full sync: session %s has nil messages", s.ID)
		}
	}

	// Incremental with future since — all unchanged.
	inc, _, err := SyncSessions(src, ver, files, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("incremental: %v", err)
	}
	if len(inc) != 2 {
		t.Fatalf("incremental = %d, want 2", len(inc))
	}
	for _, s := range inc {
		if s.Messages != nil {
			t.Errorf("incremental: session %s should have nil messages", s.ID)
		}
	}
}

// ---- Embedded schema validation tests ----

// TestEmbeddedSchemas_LoadAll loads all embedded schemas and verifies they
// parse + validate + match without errors.
func TestEmbeddedSchemas_LoadAll(t *testing.T) {
	defs, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(defs) < 3 {
		t.Fatalf("expected at least 3 embedded schemas, got %d", len(defs))
	}

	for _, ld := range defs {
		t.Run(ld.Name, func(t *testing.T) {
			if ld.Def == nil {
				t.Fatalf("schema %q failed to parse", ld.Name)
			}
			// Match() should not error (may not find files, but no code error).
			_, _, _, err := ld.Def.Match()
			if err != nil {
				// "No matching source/version" or "no supported driver" means
				// the source DB isn't present on this machine — expected on CI.
				msg := err.Error()
				if !contains(msg, "no matching source") &&
					!contains(msg, "no supported driver") {
					t.Errorf("Match() error for %q: %v", ld.Name, err)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

func osRemoveAll(paths ...string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// TestMatchError_NoSource verifies the error message format when no source DB
// is found. This ensures the TestEmbeddedSchemas_LoadAll substring checks are
// correct (regression guard for the "not detected" mismatch bug).
func TestMatchError_NoSource(t *testing.T) {
	def := &ParserDef{
		Format: 1,
		Name:   "test-nosource",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			When:   &WhenRule{FileExists: "/nonexistent/path/does/not/exist.db"},
			Paths:  []string{"/nonexistent/path/does/not/exist.db"},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					Query:        `SELECT id FROM sessions`,
					ID:           "id",
					Cursor:       "updated_at",
					CursorFormat: "rfc3339",
				},
				Messages: MessagesSpec{
					Query:   `SELECT 'user' AS role, 'hello' AS content`,
					Role:    "role",
					Content: "content",
				},
			}},
		}},
	}

	_, _, _, err := def.Match()
	if err == nil {
		t.Fatal("expected error for non-existent source, got nil")
	}
	msg := err.Error()
	if !contains(msg, "no matching source") {
		t.Errorf("error %q should contain 'no matching source'", msg)
	}
}
