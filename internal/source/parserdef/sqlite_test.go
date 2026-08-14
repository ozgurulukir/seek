package parserdef

import (
	"fmt"
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/internal/source"
)

// createFixtureDB creates an external SQLite DB mimicking the opencode schema
// with a couple of sessions and messages. Returns the DB path.
func createFixtureDB(t *testing.T, path string) {
	t.Helper()
	// Remove any existing file so we start clean.
	os.Remove(path)
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE session (
			id TEXT PRIMARY KEY,
			title TEXT,
			directory TEXT,
			parent_id TEXT,
			time_updated INTEGER
		)`,
		`CREATE TABLE message (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			data TEXT NOT NULL
		)`,
		`CREATE TABLE part (
			id TEXT PRIMARY KEY,
			message_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			data TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}

	// Session 1: a normal conversation.
	ts1 := time.Now().Add(-1 * time.Hour).UnixMilli()
	db.Exec(`INSERT INTO session (id, title, directory, parent_id, time_updated) VALUES (?, ?, ?, ?, ?)`,
		"sess-1", "Test Session One", "/home/user/project-a", "", ts1)

	// Message 1: user text.
	db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`,
		"msg-1", "sess-1", ts1, `{"role":"user"}`)
	db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, data) VALUES (?, ?, ?, ?, ?)`,
		"part-1", "msg-1", "sess-1", ts1, `{"type":"text","text":"Hello, can you help me?"}`)

	// Message 2: assistant text.
	db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`,
		"msg-2", "sess-1", ts1+1000, `{"role":"assistant"}`)
	db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, data) VALUES (?, ?, ?, ?, ?)`,
		"part-2", "msg-2", "sess-1", ts1+1000, `{"type":"text","text":"Of course! What do you need?"}`)

	// Message 3: assistant tool output (state.output path).
	db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`,
		"msg-3", "sess-1", ts1+2000, `{"role":"assistant"}`)
	db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, data) VALUES (?, ?, ?, ?, ?)`,
		"part-3", "msg-3", "sess-1", ts1+2000, `{"type":"tool","state":{"output":"Tool ran successfully"}}`)

	// Session 2: child session (parent_id set) — should still be indexed.
	ts2 := time.Now().UnixMilli()
	db.Exec(`INSERT INTO session (id, title, directory, parent_id, time_updated) VALUES (?, ?, ?, ?, ?)`,
		"sess-2", "", "/home/user/project-b", "sess-1", ts2)
	db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`,
		"msg-4", "sess-2", ts2, `{"role":"user"}`)
	db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, data) VALUES (?, ?, ?, ?, ?)`,
		"part-4", "msg-4", "sess-2", ts2, `{"type":"text","text":"Sub-agent message"}`)
}

// makeOpencodeDef builds a ParserDef pointing at a specific DB path.
func makeOpencodeDef(dbPath string) *ParserDef {
	return &ParserDef{
		Format: 1,
		Name:   "test-opencode",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{dbPath},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					Query: `SELECT id, title, directory, parent_id, time_updated FROM session`,
					ID:    "id", Title: "title",
					Cursor: "time_updated", CursorFormat: "epoch_ms",
					Metadata: map[string]string{"workspace": "directory", "parent": "parent_id"},
				},
				Messages: MessagesSpec{
					Query: `SELECT json_extract(m.data,'$.role') AS role,
                       COALESCE(json_extract(p.data,'$.text'),
                                json_extract(p.data,'$.state.output')) AS content
                        FROM message m JOIN part p ON p.message_id = m.id
                        WHERE m.session_id = :session_id
                          AND json_extract(p.data,'$.type') IN ('text','tool')
                        ORDER BY m.time_created, p.time_created`,
					Role:    "role",
					Content: "content",
				},
			}},
		}},
	}
}

// TestSQLiteDriverFullSync verifies the full sync path: detect → scan sessions → fetch messages.
func TestSQLiteDriverFullSync(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	createFixtureDB(t, dbPath)

	def := makeOpencodeDef(dbPath)
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
		t.Fatalf("unexpected session errors: %v", sErrs)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}

	// Find sess-1.
	var s1 *Session
	for i := range sessions {
		if sessions[i].ID == "sess-1" {
			s1 = &sessions[i]
		}
	}
	if s1 == nil {
		t.Fatal("sess-1 not found")
	}
	if s1.Title != "Test Session One" {
		t.Errorf("Title = %q, want 'Test Session One'", s1.Title)
	}
	if s1.Metadata["workspace"] != "/home/user/project-a" {
		t.Errorf("workspace = %q, want /home/user/project-a", s1.Metadata["workspace"])
	}
	// 3 messages: user text + assistant text + assistant tool output.
	if len(s1.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(s1.Messages))
	}
	if s1.Messages[0].Role != source.RoleUser {
		t.Errorf("msg[0] role = %q, want user", s1.Messages[0].Role)
	}
	if s1.Messages[0].Content != "Hello, can you help me?" {
		t.Errorf("msg[0] content = %q", s1.Messages[0].Content)
	}
	// Tool output via state.output COALESCE.
	if s1.Messages[2].Content != "Tool ran successfully" {
		t.Errorf("msg[2] content = %q, want 'Tool ran successfully'", s1.Messages[2].Content)
	}
}

// TestSQLiteDriverIncrementalSync verifies sessions with old cursors are skipped.
func TestSQLiteDriverIncrementalSync(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	createFixtureDB(t, dbPath)

	def := makeOpencodeDef(dbPath)
	src, ver, files, _ := def.Match()

	// First: full sync — all sessions have messages.
	all, _, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("full sync: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("full sync = %d sessions, want 2", len(all))
	}
	for _, s := range all {
		if s.Messages == nil {
			t.Errorf("full sync: session %s has nil messages", s.ID)
		}
	}

	// Now incremental with since = future (all sessions unchanged).
	inc, _, err := SyncSessions(src, ver, files, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("incremental sync: %v", err)
	}
	// All sessions returned (for orphan detection) but with nil messages.
	if len(inc) != 2 {
		t.Fatalf("incremental = %d sessions, want 2 (returned but unchanged)", len(inc))
	}
	for _, s := range inc {
		if s.Messages != nil {
			t.Errorf("incremental: session %s should have nil messages (unchanged)", s.ID)
		}
	}
}

// TestSQLiteDriverIncrementalSubSecondCursor verifies that a session with a
// sub-second cursor (epoch_ms) is correctly skipped when `since` is rebuilt from
// a millisecond-precision round-trip (the indexer stores cursor as UnixMilli).
// The indexer-level regression test (TestIndexer_ParserCollectionSync) covers
// the full end-to-end round-trip through the actual indexer code path.
func TestSQLiteDriverIncrementalSubSecondCursor(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	createFixtureDB(t, dbPath)

	def := makeOpencodeDef(dbPath)
	src, ver, files, _ := def.Match()

	// Full sync to get sessions and their cursors.
	all, _, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("full sync: %v", err)
	}

	// Find a session with a sub-second cursor component.
	var testSession *Session
	for i := range all {
		if all[i].Cursor.Nanosecond() != 0 {
			testSession = &all[i]
			break
		}
	}
	if testSession == nil {
		t.Skip("no session with sub-second cursor — fixture needs updating")
	}

	// Simulate the correct indexer round-trip: cursor → UnixMilli → since.
	since := time.UnixMilli(testSession.Cursor.UnixMilli())

	inc, _, err := SyncSessions(src, ver, files, since)
	if err != nil {
		t.Fatalf("incremental sync: %v", err)
	}
	for _, s := range inc {
		if s.ID == testSession.ID {
			if s.Messages != nil {
				t.Errorf("session %s should be unchanged but was re-indexed "+
					"(cursor=%v, since=%v)", s.ID, testSession.Cursor, since)
			}
			return
		}
	}
	t.Fatalf("session %s not returned in incremental sync", testSession.ID)
}

// TestSQLiteDriverChildSessionsIndexed verifies child sessions (parent_id set) are included.
func TestSQLiteDriverChildSessionsIndexed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	createFixtureDB(t, dbPath)

	def := makeOpencodeDef(dbPath)
	src, ver, files, _ := def.Match()
	sessions, _, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}

	var s2 *Session
	for i := range sessions {
		if sessions[i].ID == "sess-2" {
			s2 = &sessions[i]
		}
	}
	if s2 == nil {
		t.Fatal("sess-2 (child) not found — child sessions must be indexed")
	}
	if s2.Metadata["parent"] != "sess-1" {
		t.Errorf("parent metadata = %q, want sess-1", s2.Metadata["parent"])
	}
	if len(s2.Messages) != 1 {
		t.Fatalf("child messages = %d, want 1", len(s2.Messages))
	}
}

// TestSQLiteDriverDetectByVersion verifies version-level detect rules (column_exists).
func TestSQLiteDriverDetectByVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	createFixtureDB(t, dbPath)

	def := &ParserDef{
		Format: 1,
		Name:   "test-detect",
		Sources: []SourceSpec{{
			Driver: "sqlite",
			Paths:  []string{dbPath},
			Versions: []VersionSpec{
				// v2 requires a column that doesn't exist → should skip.
				{
					Version: 2,
					When:    &WhenRule{ColumnExists: [2]string{"session", "cost"}},
					Sessions: SessionsSpec{
						Query: "SELECT id FROM session", ID: "id",
					},
					Messages: MessagesSpec{Query: "SELECT 'u' AS role, 'x' AS content", Role: "role", Content: "content"},
				},
				// v1: no detect rule → fallback.
				{
					Version: 1,
					Sessions: SessionsSpec{
						Query: "SELECT id FROM session", ID: "id",
					},
					Messages: MessagesSpec{Query: "SELECT 'u' AS role, 'x' AS content", Role: "role", Content: "content"},
				},
			},
		}},
	}

	_, ver, _, err := def.Match()
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if ver.Version != 1 {
		t.Errorf("detected version = %d, want 1 (cost column missing)", ver.Version)
	}
}

// TestInlineParseZstd verifies Mode B inline parsing with zstd decode.
func TestInlineParseZstd(t *testing.T) {
	// Build a Zed-like JSON structure.
	originalJSON := []byte(`{"title":"My Thread","messages":[
		{"User":{"content":[{"Text":"What is 2+2?"}]}},
		{"Agent":{"content":[
			{"Text":"The answer is 4."},
			{"Thinking":{"text":"Let me compute this."}}
		]}}
	]}`)

	// Compress with zstd.
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	if _, err := enc.Write(originalJSON); err != nil {
		t.Fatalf("zstd write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	compressed := buf.Bytes()

	spec := &MessagesSpec{
		Inline: true,
		Decode: "zstd",
		Items:  "messages",
		Variants: []VariantSpec{
			{MatchKey: "User", Role: "user", ArrayField: "content", TextKeys: []string{"Text"}},
			{MatchKey: "Agent", Role: "assistant", ArrayField: "content", TextKeys: []string{"Text", "Thinking.text"}},
		},
	}

	msgs, err := parseInlineMessages(compressed, spec)
	if err != nil {
		t.Fatalf("parseInlineMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	if msgs[0].Role != source.RoleUser || msgs[0].Content != "What is 2+2?" {
		t.Errorf("msg[0] = {%s, %q}, want {user, 'What is 2+2?'}", msgs[0].Role, msgs[0].Content)
	}
	// Agent content should be Text + Thinking.text concatenated with \n.
	wantAgent := "The answer is 4.\nLet me compute this."
	if msgs[1].Role != source.RoleAssistant || msgs[1].Content != wantAgent {
		t.Errorf("msg[1] = {%s, %q}, want {assistant, %q}", msgs[1].Role, msgs[1].Content, wantAgent)
	}
}

// TestInlineParseWithoutDecode verifies inline parsing of plain JSON (no zstd).
func TestInlineParseWithoutDecode(t *testing.T) {
	jsonData := []byte(`{"messages":[
		{"User":{"content":[{"Text":"Hello"}]}},
		{"Agent":{"content":[{"Text":"Hi there"}]}}
	]}`)

	spec := &MessagesSpec{
		Inline: true,
		Items:  "messages",
		Variants: []VariantSpec{
			{MatchKey: "User", Role: "user", ArrayField: "content", TextKeys: []string{"Text"}},
			{MatchKey: "Agent", Role: "assistant", ArrayField: "content", TextKeys: []string{"Text"}},
		},
	}

	msgs, err := parseInlineMessages(jsonData, spec)
	if err != nil {
		t.Fatalf("parseInlineMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	if msgs[0].Content != "Hello" || msgs[1].Content != "Hi there" {
		t.Errorf("unexpected contents: %q, %q", msgs[0].Content, msgs[1].Content)
	}
}

// TestCursorNormalization verifies all cursor formats.
func TestCursorNormalization(t *testing.T) {
	tests := []struct {
		format string
		raw    string
		want   time.Time
	}{
		{"epoch_ms", "1700000000000", time.UnixMilli(1700000000000)},
		{"epoch_s", "1700000000", time.Unix(1700000000, 0)},
		{"rfc3339", "2023-11-14T22:13:20Z", time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)},
		{"datetime", "2023-11-14 22:13:20", time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got, err := normalizeCursor(tt.raw, tt.format)
			if err != nil {
				t.Fatalf("normalizeCursor: %v", err)
			}
			// Compare via Unix for timezone robustness.
			if got.Unix() != tt.want.Unix() {
				t.Errorf("Unix = %d, want %d", got.Unix(), tt.want.Unix())
			}
		})
	}
}

// TestCursorNormalizationInvalid verifies bad values error.
func TestCursorNormalizationInvalid(t *testing.T) {
	_, err := normalizeCursor("not-a-number", "epoch_ms")
	if err == nil {
		t.Fatal("expected error for invalid epoch_ms, got nil")
	}
	_, err = normalizeCursor("garbage", "rfc3339")
	if err == nil {
		t.Fatal("expected error for invalid rfc3339, got nil")
	}
}

// TestMatchExclude verifies glob exclude patterns work.
func TestMatchExclude(t *testing.T) {
	dir := t.TempDir()
	// Create main.db and main.db-wal.
	mainPath := filepath.Join(dir, "main.db")
	walPath := filepath.Join(dir, "main.db-wal")
	createFixtureDB(t, mainPath)
	if err := os.WriteFile(walPath, []byte("wal"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := matchGlob([]string{filepath.Join(dir, "*.db")}, []string{"*-wal", "*-shm"})
	if err != nil {
		t.Fatalf("matchGlob: %v", err)
	}
	// Should get main.db but NOT main.db-wal.
	found := false
	for _, f := range files {
		if f == walPath {
			t.Error("wal file should be excluded")
		}
		if f == mainPath {
			found = true
		}
	}
	if !found {
		t.Error("main.db not found in results")
	}
}
func TestBuildBatchQuery(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		want    string
		count   int
		wantErr bool
	}{
		{
			name:  "standard opencode",
			query: "SELECT role, content FROM message WHERE session_id = :session_id ORDER BY time_created",
			want:  "SELECT session_id AS _session_id, role, content FROM message WHERE session_id IN (SELECT value FROM json_each(?)) ORDER BY time_created",
			count: 1,
		},
		{
			name:  "standard copilot",
			query: "SELECT turn_index, 1 FROM turns WHERE session_id = :session_id UNION ALL SELECT turn_index, 2 FROM turns WHERE session_id = :session_id",
			want:  "SELECT session_id AS _session_id, turn_index, 1 FROM turns WHERE session_id IN (SELECT value FROM json_each(?)) UNION ALL SELECT session_id AS _session_id, turn_index, 2 FROM turns WHERE session_id IN (SELECT value FROM json_each(?))",
			count: 2,
		},
		{
			name:    "reversed bind (fails rewrite, falls back)",
			query:   "SELECT role FROM message WHERE :session_id = session_id",
			wantErr: true,
		},
		{
			name:    "at bind (fails rewrite)",
			query:   "SELECT role FROM message WHERE session_id = @session_id",
			wantErr: true,
		},
		{
			name:  "with alias",
			query: "SELECT role FROM message m WHERE m.session_id = :session_id",
			want:  "SELECT m.session_id AS _session_id, role FROM message m WHERE m.session_id IN (SELECT value FROM json_each(?))",
			count: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, count, err := buildBatchQuery(c.query)
			if (err != nil) != c.wantErr {
				t.Errorf("buildBatchQuery() error = %v, wantErr %v", err, c.wantErr)
				return
			}
			if !c.wantErr {
				if got != c.want {
					t.Errorf("buildBatchQuery() query = %v, want %v", got, c.want)
				}
				if count != c.count {
					t.Errorf("buildBatchQuery() count = %v, want %v", count, c.count)
				}
			}
		})
	}
}

func TestFetchSQLiteMessagesBatch(t *testing.T) {
	// A simple test to cover the batch boundary.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE message (session_id TEXT, role TEXT, content TEXT)")
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert 600 sessions, 1 message each
	var sessionIDs []string
	for i := 0; i < 600; i++ {
		sid := fmt.Sprintf("s%d", i)
		sessionIDs = append(sessionIDs, sid)
		db.Exec("INSERT INTO message (session_id, role, content) VALUES (?, 'user', ?)", sid, fmt.Sprintf("msg%d", i))
	}

	ver := &VersionSpec{
		Messages: MessagesSpec{
			Query:   "SELECT role, content FROM message WHERE session_id = :session_id",
			Role:    "role",
			Content: "content",
		},
	}

	// The batch function itself doesn't chunk, syncSQLiteSessions does.
	// But we can test it handles >500 fine up to limits (or we just pass a slice).
	msgMap, err := fetchSQLiteMessagesBatch(db, ver, sessionIDs[:500])
	if err != nil {
		t.Fatalf("fetchSQLiteMessagesBatch error: %v", err)
	}

	if len(msgMap) != 500 {
		t.Errorf("expected 500 mapped sessions, got %d", len(msgMap))
	}
	for i := 0; i < 500; i++ {
		sid := fmt.Sprintf("s%d", i)
		msgs := msgMap[sid]
		if len(msgs) != 1 || msgs[0].Content != fmt.Sprintf("msg%d", i) {
			t.Errorf("session %s missing or incorrect messages: %v", sid, msgs)
		}
	}

	// Test the fallback mechanism for bad query
	badVer := &VersionSpec{
		Messages: MessagesSpec{
			Query:   "SELECT role, content FROM message WHERE :session_id = session_id", // Reversed bind
			Role:    "role",
			Content: "content",
		},
	}
	_, err = fetchSQLiteMessagesBatch(db, badVer, sessionIDs[:10])
	if err == nil {
		t.Error("expected error for bad query rewrite")
	}
}
