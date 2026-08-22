package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceStartCmd_Helpers(t *testing.T) {
	// Test shellQuote function
	t.Run("shellQuote", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"/usr/local/bin/seek", "'/usr/local/bin/seek'"},
			{"/path/with spaces/seek", "'/path/with spaces/seek'"},
			{"path'with'quotes", "'path'\"'\"'with'\"'\"'quotes'"},
		}

		for _, tt := range tests {
			got := shellQuote(tt.input)
			if got != tt.expected {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		}
	})

	// Test helper paths
	t.Run("paths", func(t *testing.T) {
		home, _ := os.UserHomeDir()

		pPath := plistPath()
		expectedPlist := filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
		if pPath != expectedPlist {
			t.Errorf("plistPath() = %q, want %q", pPath, expectedPlist)
		}

		sDir := systemdDir()
		expectedSystemd := filepath.Join(home, ".config", "systemd", "user")
		if sDir != expectedSystemd {
			t.Errorf("systemdDir() = %q, want %q", sDir, expectedSystemd)
		}

		lPath := logPath()
		expectedLog := filepath.Join(home, ".cache", "seek", "service.log")
		if lPath != expectedLog {
			t.Errorf("logPath() = %q, want %q", lPath, expectedLog)
		}
	})
}
