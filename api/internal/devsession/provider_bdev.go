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

// BdevProvider manages local sessions via the bdev CLI.
type BdevProvider struct {
	logDir string
	env    string
}

func NewBdevProvider(env string) *BdevProvider {
	home, _ := os.UserHomeDir()
	if env == "" {
		env = "dev"
	}
	return &BdevProvider{
		logDir: filepath.Join(home, ".bifrost-dev", "logs"),
		env:    env,
	}
}

func (p *BdevProvider) List(ctx context.Context) ([]DevSession, error) {
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
	p.enrichLastOutput(sessions)
	for i := range sessions {
		sessions[i].Mode = ModeBdev
		sessions[i].Env = p.env
	}
	return sessions, nil
}

func (p *BdevProvider) Control(ctx context.Context, name, action string) (*ControlResponse, error) {
	switch action {
	case "start", "stop", "restart":
	case "clear-logs":
		return p.clearLogs(name)
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

func (p *BdevProvider) clearLogs(name string) (*ControlResponse, error) {
	logPath := filepath.Join(p.logDir, name+".log")
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

func (p *BdevProvider) enrichLastOutput(sessions []DevSession) {
	for i := range sessions {
		logPath := filepath.Join(p.logDir, sessions[i].Name+".log")
		info, err := os.Stat(logPath)
		if err == nil && info.Size() > 0 {
			t := info.ModTime().Unix()
			sessions[i].LastOutputAt = &t
		}
	}
}

func (p *BdevProvider) Logs(_ context.Context, name string, lines int) (*LogResponse, error) {
	if lines <= 0 {
		lines = 200
	}
	if lines > 2000 {
		lines = 2000
	}

	logPath := filepath.Join(p.logDir, name+".log")
	out, err := TailFileLines(logPath, lines)
	if err != nil {
		if os.IsNotExist(err) {
			return &LogResponse{Name: name, Lines: []string{}}, nil
		}
		return nil, fmt.Errorf("read log %s: %w", name, err)
	}
	return &LogResponse{Name: name, Lines: out}, nil
}
