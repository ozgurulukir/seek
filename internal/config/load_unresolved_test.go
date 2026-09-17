package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadUnresolvedKeepsEnvIndirection pins the M6 contract: the config a
// credential writer starts from holds raw ${VAR} references, and patching only
// the fields a command owns then saving must leave every other section's env
// indirection intact on disk — no expanded plaintext, no stale fallback copies
// (review 2026-09-17 M6).
func TestLoadUnresolvedKeepsEnvIndirection(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("DASHSCOPE_API_KEY", "expanded-literal-secret")
	t.Setenv("RR_KEY", "expanded-rerank-secret")

	dir := filepath.Join(tmpHome, ".config", "seek")
	if err := os.MkdirAll(dir, DefaultPrivateDirPerms); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	original := "embedding:\n" +
		"  base_url: https://api.example.com/v1\n" +
		"  api_key: ${DASHSCOPE_API_KEY}\n" +
		"  model: text-embedding-v4\n" +
		"ocr:\n" +
		"  enabled: true\n" +
		"rerank:\n" +
		"  api_key: ${RR_KEY}\n"
	if err := os.WriteFile(cfgPath, []byte(original), DefaultPrivateFilePerms); err != nil {
		t.Fatalf("write config: %v", err)
	}

	raw, err := LoadUnresolved()
	if err != nil {
		t.Fatalf("LoadUnresolved: %v", err)
	}
	if raw.Embedding.APIKey != "${DASHSCOPE_API_KEY}" {
		t.Fatalf("expected unexpanded embedding key, got %q", raw.Embedding.APIKey)
	}
	if raw.Rerank.APIKey != "${RR_KEY}" {
		t.Fatalf("expected unexpanded rerank key, got %q", raw.Rerank.APIKey)
	}
	// applyFallbacks copies the embedding key into unset OCR/Rerank keys; the
	// unresolved config must not.
	if raw.OCR.APIKey != "" {
		t.Fatalf("expected empty unresolved OCR key, got %q", raw.OCR.APIKey)
	}

	// Simulate the auth login write path: patch only the embedding fields,
	// persist, and inspect what lands on disk.
	raw.Embedding.APIKey = "sk-newly-typed-key"
	if err := Save(raw); err != nil {
		t.Fatalf("Save: %v", err)
	}
	onDisk, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config back: %v", err)
	}
	s := string(onDisk)
	if !strings.Contains(s, "api_key: sk-newly-typed-key") {
		t.Errorf("newly entered key not saved:\n%s", s)
	}
	if !strings.Contains(s, "${RR_KEY}") {
		t.Errorf("rerank ${VAR} indirection destroyed by save:\n%s", s)
	}
	for _, leaked := range []string{"expanded-literal-secret", "expanded-rerank-secret"} {
		if strings.Contains(s, leaked) {
			t.Errorf("expanded secret %q baked into config file:\n%s", leaked, s)
		}
	}
}

// TestLoadUnresolvedFailsOnUnreadableConfig pins the silent-failure-audit fix:
// a config.yaml that exists but cannot be read must abort LoadUnresolved with
// an error — treating the failure as "no config" would let a credential write
// overwrite the whole file with defaults (review 2026-09-17 M6 follow-up).
func TestLoadUnresolvedFailsOnUnreadableConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Make the config path a directory: any ReadFile on it fails with an
	// error that is not "not exist", on every platform.
	cfgPath := filepath.Join(tmpHome, ".config", "seek", "config.yaml")
	if err := os.MkdirAll(cfgPath, DefaultPrivateDirPerms); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := LoadUnresolved(); err == nil {
		t.Fatal("expected error for unreadable config.yaml, got nil")
	}
}
