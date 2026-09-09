package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceTemplates_RunSyncOnce(t *testing.T) {
	data := struct {
		Label    string
		Binary   string
		Interval int
		LogPath  string
	}{
		Label:    serviceLabel,
		Binary:   "/path with spaces/seek",
		Interval: 3600,
		LogPath:  "/tmp/seek.log",
	}

	var plist bytes.Buffer
	if err := plistTemplate.Execute(&plist, data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plist.String(), "&&") {
		t.Fatalf("launchd template still chains a second child command:\n%s", plist.String())
	}
	if got := strings.Count(plist.String(), " sync"); got != 1 {
		t.Fatalf("launchd template contains %d sync commands, want 1", got)
	}

	var systemd bytes.Buffer
	if err := systemdServiceTemplate.Execute(&systemd, data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(systemd.String(), "\nExecStart=\""+data.Binary+"\" embed") {
		t.Fatalf("systemd template still starts a child embed command:\n%s", systemd.String())
	}
	if got := strings.Count(systemd.String(), "ExecStart="); got != 1 {
		t.Fatalf("systemd template contains %d ExecStart entries, want 1", got)
	}

	if got, want := fmt.Sprintf(`cmd.exe /c ""%s" sync"`, data.Binary), `cmd.exe /c ""/path with spaces/seek" sync"`; got != want {
		t.Fatalf("windows task command = %q, want %q", got, want)
	}
}

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
