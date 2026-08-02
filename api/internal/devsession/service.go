package devsession

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Service manages dev sessions via the bdev CLI.
type Service struct {
	logDir string
}

func NewService() *Service {
	home, _ := os.UserHomeDir()
	return &Service{
		logDir: filepath.Join(home, ".bifrost-dev", "logs"),
	}
}

// List runs `bdev status --json` and returns parsed sessions.
func (s *Service) List(ctx context.Context) ([]DevSession, error) {
	cmd := exec.CommandContext(ctx, "bdev", "status", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("bdev status: %s", msg)
	}

	var sessions []DevSession
	if err := json.Unmarshal(stdout.Bytes(), &sessions); err != nil {
		return nil, fmt.Errorf("parse bdev output: %w", err)
	}
	s.enrichLastOutput(sessions)
	return sessions, nil
}

// Control runs `bdev <action> <name>` and captures the result.
func (s *Service) Control(ctx context.Context, name, action string) (*ControlResponse, error) {
	switch action {
	case "start", "stop", "restart":
	case "clear-logs":
		return s.clearLogs(name)
	default:
		return &ControlResponse{
			Name: name, Action: action, Success: false,
			Message: "unknown action: " + action,
		}, fmt.Errorf("unknown action: %s", action)
	}

	cmd := exec.CommandContext(ctx, "bdev", action, name)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return &ControlResponse{
			Name: name, Action: action, Success: false, Message: msg,
		}, nil
	}

	msg := strings.TrimSpace(stdout.String())
	if msg == "" {
		msg = action + " completed"
	}
	return &ControlResponse{
		Name: name, Action: action, Success: true, Message: msg,
	}, nil
}

// clearLogs truncates the log file to zero bytes.
func (s *Service) clearLogs(name string) (*ControlResponse, error) {
	logPath := filepath.Join(s.logDir, name+".log")
	if err := os.Truncate(logPath, 0); err != nil {
		if os.IsNotExist(err) {
			return &ControlResponse{Name: name, Action: "clear-logs", Success: true, Message: "no log file"}, nil
		}
		return &ControlResponse{
			Name: name, Action: "clear-logs", Success: false, Message: err.Error(),
		}, nil
	}
	return &ControlResponse{Name: name, Action: "clear-logs", Success: true, Message: "log cleared"}, nil
}

// enrichLastOutput populates LastOutputAt from the log file mtime.
func (s *Service) enrichLastOutput(sessions []DevSession) {
	for i := range sessions {
		logPath := filepath.Join(s.logDir, sessions[i].Name+".log")
		info, err := os.Stat(logPath)
		if err == nil && info.Size() > 0 {
			t := info.ModTime().Unix()
			sessions[i].LastOutputAt = &t
		}
	}
}

// Logs reads the last N lines from ~/.bifrost-dev/logs/<name>.log
// using a tail seek (does not load the whole file when large).
func (s *Service) Logs(_ context.Context, name string, lines int) (*LogResponse, error) {
	if lines <= 0 {
		lines = 200
	}
	if lines > 2000 {
		lines = 2000
	}

	logPath := filepath.Join(s.logDir, name+".log")
	out, err := TailFileLines(logPath, lines)
	if err != nil {
		if os.IsNotExist(err) {
			return &LogResponse{Name: name, Lines: []string{}}, nil
		}
		return nil, fmt.Errorf("read log %s: %w", name, err)
	}
	return &LogResponse{Name: name, Lines: out}, nil
}
