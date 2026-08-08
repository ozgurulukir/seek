package parserdef

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ozgurulukir/seek/internal/source"
)

// ---- JSONL fixture helpers ----

// writeJSONLFile writes lines to a .jsonl file and returns its path.
func writeJSONLFile(t *testing.T, dir, name string, lines []string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	// Ensure consistent mtime (1 second ago) for cursor testing.
	mtime := time.Now().Add(-1 * time.Second)
	os.Chtimes(path, mtime, mtime)
	return path
}

// ---- Claude-style JSONL tests (asymmetric content, text blocks) ----

func TestJSONL_ClaudeStyle_AsymmetricContent(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		// Non-message lines (should be ignored).
		`{"type":"mode","mode":"normal"}`,
		`{"type":"summary","summary":"Test conversation"}`,
		// User message: content at message.content (string form).
		`{"type":"user","message":{"role":"user","content":"What is the error?"},"cwd":"/home/user/project"}`,
		// Assistant message: content at message.content (array form with text blocks).
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Let me think..."},{"type":"text","text":"The error is a null pointer."}]}}`,
		// User message: content at message.content (array form).
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"How to fix it?"}]}}`,
		// Assistant message with only tool_use (no text) → should be excluded.
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"read_file"}]}}`,
		// Assistant with text + tool_use → only text included.
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Add a null check."},{"type":"tool_use","name":"edit_file"}]}}`,
	}
	writeJSONLFile(t, dir, "test-session.jsonl", lines)

	def := &ParserDef{
		Format: 1,
		Name:   "test-claude",
		Sources: []SourceSpec{{
			Driver: "jsonl",
			Paths:  []string{dir},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					IDFromFilename:  true,
					CursorFromMtime: true,
					CursorFormat:    "epoch_s",
					Metadata:        map[string]string{"workspace": "cwd"},
				},
				Messages: MessagesSpec{
					LineFilter:      []string{"user", "assistant"},
					RoleField:       "type",
					ContentPath:     "message.content",
					ContentPathUser: "message.content",
					TextTypes:       []string{"text"},
				},
			}},
		}},
	}

	src, ver, files, err := def.Match()
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}

	sessions, sErrs, err := syncJSONLSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("syncJSONLSessions: %v", err)
	}
	if len(sErrs) != 0 {
		t.Fatalf("session errors: %v", sErrs)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}

	sess := sessions[0]
	// ID from filename.
	if sess.ID != "test-session" {
		t.Errorf("ID = %q, want test-session", sess.ID)
	}
	// Metadata workspace from cwd.
	if sess.Metadata["workspace"] != "/home/user/project" {
		t.Errorf("workspace = %q", sess.Metadata["workspace"])
	}
	// Cursor from mtime (non-zero).
	if sess.Cursor.IsZero() {
		t.Error("cursor should be non-zero (from mtime)")
	}

	// 4 messages expected (thinking/tool_use excluded):
	// user "What is the error?"
	// assistant "The error is a null pointer."
	// user "How to fix it?"
	// assistant "Add a null check."
	// (the assistant-only-tool_use line produces no message)
	if len(sess.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(sess.Messages))
	}

	expected := []struct{ role, content string }{
		{source.RoleUser, "What is the error?"},
		{source.RoleAssistant, "The error is a null pointer."},
		{source.RoleUser, "How to fix it?"},
		{source.RoleAssistant, "Add a null check."},
	}
	for i, e := range expected {
		if sess.Messages[i].Role != e.role {
			t.Errorf("msg[%d] role = %q, want %q", i, sess.Messages[i].Role, e.role)
		}
		if sess.Messages[i].Content != e.content {
			t.Errorf("msg[%d] content = %q, want %q", i, sess.Messages[i].Content, e.content)
		}
	}
}

func TestJSONL_ClaudeStyle_EmptySessionSkipped(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"type":"mode","mode":"normal"}`,
		`{"type":"system","content":"system prompt"}`,
	}
	writeJSONLFile(t, dir, "empty-session.jsonl", lines)

	def := &ParserDef{
		Format: 1,
		Name:   "test-empty",
		Sources: []SourceSpec{{
			Driver: "jsonl",
			Paths:  []string{dir},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					IDFromFilename:  true,
					CursorFromMtime: true,
					CursorFormat:    "epoch_s",
				},
				Messages: MessagesSpec{
					LineFilter:  []string{"user", "assistant"},
					RoleField:   "type",
					ContentPath: "message.content",
					TextTypes:   []string{"text"},
				},
			}},
		}},
	}

	src, ver, files, _ := def.Match()
	sessions, _, err := syncJSONLSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("syncJSONLSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	// Session exists but has 0 messages (empty session).
	if len(sessions[0].Messages) != 0 {
		t.Errorf("messages = %d, want 0 (no user/assistant lines)", len(sessions[0].Messages))
	}
}

// TestJSONL_ClaudeStyle_LegacyFormat verifies the content_path fallback handles
// the legacy Claude format where assistant message IS the content directly
// (array or string at "message", not nested under "message.content").
// This format appears in older Claude Code exports and native parser test fixtures.
func TestJSONL_ClaudeStyle_LegacyFormat(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		// Legacy user: content at message.content (string).
		`{"type":"user","message":{"role":"user","content":"Hello!"}}`,
		// Legacy assistant: content is a direct array at "message".
		`{"type":"assistant","message":[{"type":"text","text":"Direct array response"}]}`,
		// Legacy assistant: content is a bare string at "message".
		`{"type":"assistant","message":"Bare string response"}`,
		// Modern assistant: content at message.content (wrapper dict).
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Wrapper dict response"}]}}`,
	}
	writeJSONLFile(t, dir, "legacy-session.jsonl", lines)

	def := &ParserDef{
		Format: 1,
		Name:   "test-legacy-claude",
		Sources: []SourceSpec{{
			Driver: "jsonl",
			Paths:  []string{dir},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					IDFromFilename:  true,
					CursorFromMtime: true,
					CursorFormat:    "epoch_s",
				},
				Messages: MessagesSpec{
					LineFilter:      []string{"user", "assistant"},
					RoleField:       "type",
					ContentPath:     "message.content,message",
					ContentPathUser: "message.content,message",
					TextTypes:       []string{"text"},
				},
			}},
		}},
	}

	src, ver, files, err := def.Match()
	if err != nil {
		t.Fatalf("Match: %v", err)
	}

	sessions, sErrs, err := syncJSONLSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("syncJSONLSessions: %v", err)
	}
	if len(sErrs) > 0 {
		t.Fatalf("session errors: %v", sErrs)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}

	sess := sessions[0]
	// All 4 messages must be extracted (1 user + 3 assistant in different formats).
	if len(sess.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(sess.Messages))
		for i, m := range sess.Messages {
			t.Logf("  [%d] %s: %q", i, m.Role, m.Content)
		}
	}

	expected := []struct{ role, content string }{
		{source.RoleUser, "Hello!"},
		{source.RoleAssistant, "Direct array response"},
		{source.RoleAssistant, "Bare string response"},
		{source.RoleAssistant, "Wrapper dict response"},
	}
	for i, e := range expected {
		if sess.Messages[i].Role != e.role {
			t.Errorf("msg[%d] role = %q, want %q", i, sess.Messages[i].Role, e.role)
		}
		if sess.Messages[i].Content != e.content {
			t.Errorf("msg[%d] content = %q, want %q", i, sess.Messages[i].Content, e.content)
		}
	}
}

// ---- Codex-style JSONL tests (multi-line-type, payload) ----

func TestJSONL_CodexStyle_MultiLineType(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		// Session metadata line (carries session ID).
		`{"type":"session_meta","payload":{"id":"codex-session-123"}}`,
		// Response item: user message.
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Explain this function"}]}}`,
		// Response item: assistant message.
		`{"type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"This function computes the factorial."}]}}`,
		// Response item: system role → excluded.
		`{"type":"response_item","payload":{"role":"system","content":[{"type":"output_text","text":"hidden system msg"}]}}`,
		// Response item: function_call → excluded.
		`{"type":"response_item","payload":{"role":"assistant","content":[{"type":"function_call","name":"compute"}]}}`,
	}
	path := writeJSONLFile(t, dir, "codex-123.jsonl", lines)

	def := &ParserDef{
		Format: 1,
		Name:   "test-codex",
		Sources: []SourceSpec{{
			Driver: "jsonl",
			Paths:  []string{dir},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					ID:              "payload.id",
					CursorFromMtime: true,
					CursorFormat:    "epoch_s",
				},
				Messages: MessagesSpec{
					LineFilter:  []string{"response_item"},
					RoleField:   "payload.role",
					ContentPath: "payload.content",
					TextTypes:   []string{"input_text", "output_text"},
				},
			}},
		}},
	}

	src, ver, files, err := def.Match()
	if err != nil {
		t.Fatalf("Match: %v", err)
	}

	sessions, sErrs, err := syncJSONLSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("syncJSONLSessions: %v", err)
	}
	if len(sErrs) != 0 {
		t.Fatalf("session errors: %v", sErrs)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}

	sess := sessions[0]
	// ID from session_meta payload.id.
	if sess.ID != "codex-session-123" {
		t.Errorf("ID = %q, want codex-session-123", sess.ID)
	}
	// SrcPath should be the file path.
	if sess.SrcPath != path {
		t.Errorf("SrcPath = %q, want %q", sess.SrcPath, path)
	}

	// 2 messages: user + assistant (system excluded by role gate, function_call excluded by text_types).
	if len(sess.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(sess.Messages))
	}
	if sess.Messages[0].Role != source.RoleUser || sess.Messages[0].Content != "Explain this function" {
		t.Errorf("msg[0] = %v/%q", sess.Messages[0].Role, sess.Messages[0].Content)
	}
	if sess.Messages[1].Role != source.RoleAssistant || sess.Messages[1].Content != "This function computes the factorial." {
		t.Errorf("msg[1] = %v/%q", sess.Messages[1].Role, sess.Messages[1].Content)
	}
}

// ---- Incremental sync test ----

func TestJSONL_IncrementalSync(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"Hello"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi there!"}]}}`,
	}
	writeJSONLFile(t, dir, "inc-session.jsonl", lines)

	def := &ParserDef{
		Format: 1,
		Name:   "test-inc",
		Sources: []SourceSpec{{
			Driver: "jsonl",
			Paths:  []string{dir},
			Versions: []VersionSpec{{
				Version: 1,
				Sessions: SessionsSpec{
					IDFromFilename:  true,
					CursorFromMtime: true,
					CursorFormat:    "epoch_s",
				},
				Messages: MessagesSpec{
					LineFilter:  []string{"user", "assistant"},
					RoleField:   "type",
					ContentPath: "message.content",
					TextTypes:   []string{"text"},
				},
			}},
		}},
	}

	src, ver, files, _ := def.Match()

	// Full sync: all messages.
	all, _, err := syncJSONLSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("full sync: %v", err)
	}
	for _, s := range all {
		if s.Messages == nil {
			t.Errorf("full sync: session %s has nil messages", s.ID)
		}
	}

	// Incremental with future since — all unchanged.
	inc, _, err := syncJSONLSessions(src, ver, files, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("incremental: %v", err)
	}
	if len(inc) != 1 {
		t.Fatalf("incremental = %d, want 1", len(inc))
	}
	for _, s := range inc {
		if s.Messages != nil {
			t.Errorf("incremental: session %s should have nil messages", s.ID)
		}
	}
}

// ---- Recursive discovery + exclude test ----

func TestJSONL_RecursiveDiscovery(t *testing.T) {
	root := t.TempDir()
	// Create nested directories with JSONL files.
	subdir := filepath.Join(root, "project-a")
	os.MkdirAll(subdir, 0755)
	nestedSub := filepath.Join(root, "project-b", "deep")
	os.MkdirAll(nestedSub, 0755)

	writeJSONLFile(t, subdir, "session1.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":"msg1"}}`,
	})
	writeJSONLFile(t, nestedSub, "session2.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":"msg2"}}`,
	})
	// Non-jsonl file should be ignored.
	os.WriteFile(filepath.Join(root, "readme.txt"), []byte("not jsonl"), 0644)
	// Excluded file.
	writeJSONLFile(t, root, "session_index.jsonl", []string{
		`{"id":"123","thread_name":"should be excluded"}`,
	})

	files, err := walkJSONLFiles([]string{root}, []string{"session_index.jsonl"})
	if err != nil {
		t.Fatalf("walkJSONLFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2 (session1 + session2, session_index excluded)", len(files))
	}
}

// ---- Embedded schema validation tests ----

func TestEmbeddedSchemas_LoadAllWithJSONL(t *testing.T) {
	defs, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(defs) < 5 {
		t.Fatalf("expected at least 5 embedded schemas, got %d", len(defs))
	}

	for _, ld := range defs {
		t.Run(ld.Name, func(t *testing.T) {
			if ld.Def == nil {
				t.Fatalf("schema %q failed to parse", ld.Name)
			}
			// Match() should not error (may not find files, but no code error).
			_, _, _, err := ld.Def.Match()
			if err != nil {
				msg := err.Error()
				if !contains(msg, "no matching source") &&
					!contains(msg, "no supported driver") {
					t.Errorf("Match() error for %q: %v", ld.Name, err)
				}
			}
		})
	}
}

// ---- Real DB field test ----

func TestRealDB_ClaudeSchema(t *testing.T) {
	home, _ := os.UserHomeDir()
	projectsDir := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(projectsDir); err != nil {
		t.Skip("no ~/.claude/projects on this machine")
	}

	def, err := Load("claude")
	if err != nil {
		t.Fatalf("load claude: %v", err)
	}
	src, ver, files, err := def.Match()
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if src.Driver != "jsonl" {
		t.Fatalf("driver = %q, want jsonl", src.Driver)
	}
	if len(files) == 0 {
		t.Fatal("no source files detected")
	}

	sessions, sErrs, err := syncJSONLSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(sErrs) > 0 {
		t.Fatalf("session errors: %v", sErrs)
	}
	if len(sessions) == 0 {
		t.Fatal("no sessions indexed")
	}

	// At least one session must have messages with text content.
	hasContent := false
	for _, s := range sessions {
		for _, m := range s.Messages {
			if m.Content != "" {
				hasContent = true
				break
			}
		}
	}
	if !hasContent {
		t.Error("no message content found across all sessions")
	}

	// At least one session should have workspace metadata (cwd field).
	hasWorkspace := false
	for _, s := range sessions {
		if s.Metadata["workspace"] != "" {
			hasWorkspace = true
			break
		}
	}
	if !hasWorkspace {
		t.Error("no workspace metadata found (cwd field)")
	}

	t.Logf("claude schema: %d sessions, %d messages total",
		len(sessions), countMessages(sessions))
}
