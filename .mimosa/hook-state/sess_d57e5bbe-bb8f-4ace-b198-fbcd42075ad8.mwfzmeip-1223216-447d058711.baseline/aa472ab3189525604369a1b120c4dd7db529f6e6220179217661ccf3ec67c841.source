package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/agenthooks"
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

	winAction, err := windowsTaskAction(data.Binary)
	if err != nil {
		t.Fatalf("windowsTaskAction error: %v", err)
	}
	if want := `"/path with spaces/seek" sync`; winAction != want {
		t.Fatalf("windows task action = %q, want %q", winAction, want)
	}
	// The action must go through CreateProcess directly: cmd.exe would expand
	// %VAR% even inside double quotes and treat embedded quotes as syntax.
	if strings.Contains(winAction, "cmd.exe") {
		t.Fatalf("windows task action still routes through cmd.exe: %q", winAction)
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
			got := agenthooks.ShellQuote(tt.input)
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

func TestWindowsTaskAction(t *testing.T) {
	tests := []struct {
		name      string
		bin       string
		want      string
		expectErr bool
	}{
		{
			name: "simple path without spaces",
			bin:  `C:\ProgramFiles\seek\seek.exe`,
			want: `C:\ProgramFiles\seek\seek.exe sync`,
		},
		{
			name: "path with spaces",
			bin:  `C:\Program Files\seek\seek.exe`,
			want: `"C:\Program Files\seek\seek.exe" sync`,
		},
		{
			name: "path with trailing backslash and space",
			bin:  `C:\Program Files\seek\`,
			want: `"C:\Program Files\seek\\" sync`,
		},
		{
			name: "path with double quotes inside spaces",
			bin:  `C:\Program Files\"seek"\seek.exe`,
			want: `"C:\Program Files\\\"seek\"\seek.exe" sync`,
		},
		{
			name:      "path with null byte",
			bin:       "C:\\seek\x00.exe",
			expectErr: true,
		},
		{
			name:      "path with newline",
			bin:       "C:\\seek\n.exe",
			expectErr: true,
		},
		{
			name:      "path with carriage return",
			bin:       "C:\\seek\r.exe",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowsTaskAction(tt.bin)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("windowsTaskAction(%q) expected error, got nil", tt.bin)
				}
				return
			}
			if err != nil {
				t.Fatalf("windowsTaskAction(%q) unexpected error: %v", tt.bin, err)
			}
			if got != tt.want {
				t.Errorf("windowsTaskAction(%q) = %q, want %q", tt.bin, got, tt.want)
			}
		})
	}
}
