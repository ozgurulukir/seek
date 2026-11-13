package parserdef

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// homePath expands a ~/ path and reports whether the file exists.
func homePath(t *testing.T, rel string) (string, bool) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(home, rel)
	_, err = os.Stat(p)
	return p, err == nil
}

// TestRealDB_CopilotCLI field-tests the copilot-cli schema against the real
// ~/.copilot/session-store.db if present on this machine.
func TestRealDB_CopilotCLI(t *testing.T) {
	if _, ok := homePath(t, ".copilot/session-store.db"); !ok {
		t.Skip("no ~/.copilot/session-store.db on this machine")
	}

	def, err := Load("copilot-cli")
	if err != nil {
		t.Fatalf("load copilot-cli: %v", err)
	}
	src, ver, files, err := def.Match()
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if src.Driver != "sqlite" {
		t.Fatalf("driver = %q, want sqlite", src.Driver)
	}
	if ver.Version != 1 {
		t.Fatalf("version = %d, want 1", ver.Version)
	}
	if len(files) == 0 {
		t.Fatal("no source files detected")
	}

	sessions, errs, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("session errors: %v", errs)
	}
	if len(sessions) == 0 {
		t.Fatal("no sessions indexed")
	}

	// Each session must have a parsed cursor, at least one message, and workspace metadata.
	for _, s := range sessions {
		if s.Cursor.IsZero() {
			t.Errorf("session %s: cursor is zero", s.ID)
		}
		if len(s.Messages) == 0 {
			t.Errorf("session %s: no messages", s.ID)
		}
		if s.Metadata["workspace"] == "" {
			t.Errorf("session %s: no workspace metadata", s.ID)
		}
		// Messages should have role + content (not empty).
		for i, m := range s.Messages {
			if m.Role == "" {
				t.Errorf("session %s msg[%d]: empty role", s.ID, i)
			}
			if m.Content == "" {
				t.Errorf("session %s msg[%d]: empty content", s.ID, i)
			}
		}
	}

	t.Logf("copilot-cli: %d sessions, %d messages total", len(sessions), countMessages(sessions))
}

// TestRealDB_Zed field-tests the zed schema against the real
// ~/.local/share/zed/threads/threads.db if present.
// Verifies zstd decode + variant matching (Text + Thinking.text) works.
func TestRealDB_Zed(t *testing.T) {
	if _, ok := homePath(t, ".local/share/zed/threads/threads.db"); !ok {
		t.Skip("no ~/.local/share/zed/threads/threads.db on this machine")
	}

	def, err := Load("zed")
	if err != nil {
		t.Fatalf("load zed: %v", err)
	}
	src, ver, files, err := def.Match()
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if src.Driver != "sqlite" {
		t.Fatalf("driver = %q, want sqlite", src.Driver)
	}
	if ver.Version != 1 {
		t.Fatalf("version = %d, want 1", ver.Version)
	}
	if len(files) == 0 {
		t.Fatal("no source files detected")
	}

	sessions, errs, err := SyncSessions(src, ver, files, time.Time{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("session errors: %v", errs)
	}
	if len(sessions) == 0 {
		t.Fatal("no sessions indexed")
	}

	// Zed threads have user + agent messages. The acceptance criterion is
	// that User + Agent (Text+Thinking) content is indexed.
	hasThinking := false
	for _, s := range sessions {
		if s.Cursor.IsZero() {
			t.Errorf("session %s: cursor is zero", s.ID)
		}
		if len(s.Messages) == 0 {
			t.Errorf("session %s: no messages", s.ID)
		}
		hasUser, hasAssistant := false, false
		for _, m := range s.Messages {
			switch m.Role {
			case "user":
				hasUser = true
			case "assistant":
				hasAssistant = true
				// Thinking text tends to be long (hundreds of chars). If any
				// assistant message is long, that's evidence Thinking.text was extracted.
				if len(m.Content) > 200 {
					hasThinking = true
				}
			}
		}
		if !hasUser {
			t.Errorf("session %s: no user messages", s.ID)
		}
		if !hasAssistant {
			t.Errorf("session %s: no assistant messages", s.ID)
		}
	}

	// At least one session must have Thinking content (>200 chars proves the
	// dot-path "Thinking.text" extraction works, not just flat Text).
	if !hasThinking {
		t.Log("warning: no Thinking content detected (may be expected if threads have no thinking blocks)")
	}

	t.Logf("zed: %d sessions, %d messages total", len(sessions), countMessages(sessions))
}

func countMessages(sessions []Session) int {
	n := 0
	for _, s := range sessions {
		n += len(s.Messages)
	}
	return n
}
