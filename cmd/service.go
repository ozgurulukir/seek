package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/ozgurulukir/seek/internal/config"
)

const (
	serviceLabel = "io.github.ethan-huo.seek"
	windowsTask  = "SeekSync"
)

var plistTemplate = template.Must(template.New("plist").Funcs(template.FuncMap{
	"shellQuote": shellQuote,
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>/bin/sh</string>
		<string>-c</string>
		<string>{{shellQuote .Binary}} sync && {{shellQuote .Binary}} embed</string>
	</array>
	<key>StartInterval</key>
	<integer>{{.Interval}}</integer>
	<key>StandardOutPath</key>
	<string>{{.LogPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{.LogPath}}</string>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`))

// shellQuote wraps a string in single quotes for safe shell embedding,
// escaping any embedded single quotes. This prevents shell injection when
// binary paths contain spaces or special characters.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

var systemdServiceTemplate = template.Must(template.New("systemdService").Parse(`[Unit]
Description=Seek periodic sync and embed
After=network.target

[Service]
Type=oneshot
ExecStart="{{.Binary}}" sync
ExecStart="{{.Binary}}" embed
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}
`))

var systemdTimerTemplate = template.Must(template.New("systemdTimer").Parse(`[Unit]
Description=Run seek periodic sync and embed

[Timer]
OnBootSec=1min
OnUnitActiveSec={{.Interval}}s
Unit=seek.service

[Install]
WantedBy=timers.target
`))

type ServiceCmd struct {
	Start  ServiceStartCmd  `cmd:"" help:"Start periodic sync+embed (Task Scheduler / systemd / launchd)"`
	Stop   ServiceStopCmd   `cmd:"" help:"Stop periodic sync+embed"`
	Status ServiceStatusCmd `cmd:"" help:"Show service status"`
}

type ServiceStartCmd struct {
	Interval int `short:"i" default:"3600" help:"Sync interval in seconds"`
}

type ServiceStopCmd struct{}

type ServiceStatusCmd struct{}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
}

func systemdDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user")
}

func logPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "seek", "service.log")
}

func seekBinary() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			return real
		}
		return exe
	}
	bin, err := exec.LookPath("seek")
	if err != nil {
		return "seek"
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return bin
	}
	return real
}

func (c *ServiceStartCmd) Run(cfg *config.AppConfig) error {
	interval := c.Interval
	if interval < 60 {
		interval = 60
	}

	_ = os.MkdirAll(filepath.Dir(logPath()), config.DefaultDirPerms)
	bin := seekBinary()

	switch runtime.GOOS {
	case "windows":
		minutes := interval / 60
		if minutes < 1 {
			minutes = 1
		}
		trArg := fmt.Sprintf(`cmd.exe /c ""%s" sync && "%s" embed"`, bin, bin)
		out, err := exec.Command("schtasks", "/Create", "/F", "/SC", "MINUTE", "/MO", fmt.Sprintf("%d", minutes), "/TN", windowsTask, "/TR", trArg).CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks create: %s (%w)", string(out), err)
		}
		fmt.Printf("Service scheduled via Windows Task Scheduler (every %d min)\n", minutes)
		fmt.Printf("  Task:   %s\n", windowsTask)
		fmt.Printf("  Binary: %s\n", bin)
		return nil

	case "linux":
		dir := systemdDir()
		if err := os.MkdirAll(dir, config.DefaultDirPerms); err != nil {
			return fmt.Errorf("create systemd user dir: %w", err)
		}

		servicePath := filepath.Join(dir, "seek.service")
		timerPath := filepath.Join(dir, "seek.timer")

		data := struct {
			Binary   string
			Interval int
			LogPath  string
		}{
			Binary:   bin,
			Interval: interval,
			LogPath:  logPath(),
		}

		var sBuf bytes.Buffer
		if err := systemdServiceTemplate.Execute(&sBuf, data); err != nil {
			return fmt.Errorf("render systemd service: %w", err)
		}
		if err := os.WriteFile(servicePath, sBuf.Bytes(), config.DefaultFilePerms); err != nil {
			return fmt.Errorf("write systemd service: %w", err)
		}

		var tBuf bytes.Buffer
		if err := systemdTimerTemplate.Execute(&tBuf, data); err != nil {
			return fmt.Errorf("render systemd timer: %w", err)
		}
		if err := os.WriteFile(timerPath, tBuf.Bytes(), config.DefaultFilePerms); err != nil {
			return fmt.Errorf("write systemd timer: %w", err)
		}

		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		out, err := exec.Command("systemctl", "--user", "enable", "--now", "seek.timer").CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl enable seek.timer: %s (%w)", string(out), err)
		}

		fmt.Printf("Service started via systemd user timer (every %ds)\n", interval)
		fmt.Printf("  Unit: %s\n", timerPath)
		fmt.Printf("  Log:  %s\n", logPath())
		return nil

	case "darwin":
		path := plistPath()
		_ = os.MkdirAll(filepath.Dir(path), config.DefaultDirPerms)

		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create plist: %w", err)
		}

		data := struct {
			Label    string
			Binary   string
			Interval int
			LogPath  string
		}{
			Label:    serviceLabel,
			Binary:   bin,
			Interval: interval,
			LogPath:  logPath(),
		}

		if err := plistTemplate.Execute(f, data); err != nil {
			f.Close()
			return fmt.Errorf("write plist: %w", err)
		}
		f.Close()

		runLaunchctl("bootout", fmt.Sprintf("gui/%d", os.Getuid()), path)
		out, err := runLaunchctl("bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), path)
		if err != nil {
			return fmt.Errorf("launchctl bootstrap: %s (%w)", string(out), err)
		}

		fmt.Printf("Service started via launchd (every %ds)\n", interval)
		fmt.Printf("  Plist: %s\n", path)
		fmt.Printf("  Log:   %s\n", logPath())
		return nil

	default:
		return fmt.Errorf("unsupported operating system for background service: %s", runtime.GOOS)
	}
}

func (c *ServiceStopCmd) Run(cfg *config.AppConfig) error {
	switch runtime.GOOS {
	case "windows":
		if err := exec.Command("schtasks", "/Query", "/TN", windowsTask).Run(); err != nil {
			fmt.Println("Service not installed.")
			return nil
		}
		out, err := exec.Command("schtasks", "/Delete", "/F", "/TN", windowsTask).CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks delete: %s (%w)", string(out), err)
		}
		fmt.Println("Service stopped and removed from Task Scheduler.")
		return nil

	case "linux":
		dir := systemdDir()
		timerPath := filepath.Join(dir, "seek.timer")
		servicePath := filepath.Join(dir, "seek.service")

		_ = exec.Command("systemctl", "--user", "disable", "--now", "seek.timer").Run()
		_ = os.Remove(timerPath)
		_ = os.Remove(servicePath)
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

		fmt.Println("Service stopped and systemd units removed.")
		return nil

	case "darwin":
		path := plistPath()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Println("Service not installed.")
			return nil
		}

		out, err := runLaunchctl("bootout", fmt.Sprintf("gui/%d", os.Getuid()), path)
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 3 {
				// Already not loaded
			} else {
				fmt.Printf("  WARN: launchctl bootout: %s\n", string(out))
			}
		}

		_ = os.Remove(path)
		fmt.Println("Service stopped and removed.")
		return nil

	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

func (c *ServiceStatusCmd) Run(cfg *config.AppConfig) error {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("schtasks", "/Query", "/TN", windowsTask, "/FO", "LIST").CombinedOutput()
		if err != nil {
			fmt.Println("Service not installed in Task Scheduler. Run: seek service start")
			return nil
		}
		fmt.Println("Service status (Windows Task Scheduler):")
		fmt.Println(strings.TrimSpace(string(out)))
		return nil

	case "linux":
		out, err := exec.Command("systemctl", "--user", "status", "seek.timer").CombinedOutput()
		if err != nil {
			fmt.Println("Service not active. Run: seek service start")
			return nil
		}
		fmt.Println(strings.TrimSpace(string(out)))
		return nil

	case "darwin":
		path := plistPath()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Println("Service not installed. Run: seek service start")
			return nil
		}

		out, err := runLaunchctl("print", fmt.Sprintf("gui/%d/%s", os.Getuid(), serviceLabel))
		if err != nil {
			fmt.Println("Service installed but not running.")
			fmt.Printf("  Plist: %s\n", path)
			return nil
		}

		fmt.Println("Service running.")
		fmt.Printf("  Plist: %s\n", path)
		fmt.Printf("  Log:   %s\n", logPath())
		_ = out
		return nil

	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

func runLaunchctl(args ...string) ([]byte, error) {
	for _, arg := range args {
		if strings.ContainsRune(arg, '\x00') {
			return nil, fmt.Errorf("launchctl arg %q contains null byte", arg)
		}
	}
	return exec.Command("launchctl", args...).CombinedOutput()
}
