// Package launchd lists this machine's com.bifrost.* launchd agents.
//
// The operator plane on each Mac mini serves the result at
// GET /api/v1/agent/launchd. The nightly maintainer reconcile reads it.
// Nothing here talks to the cluster.
package launchd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Service is one com.bifrost.* agent. Running is false when launchctl has
// no PID, which is normal for a StartInterval job between runs.
type Service struct {
	Label    string `json:"label"`
	PID      int    `json:"pid"`
	LastExit int    `json:"last_exit"`
	Running  bool   `json:"running"`
	Plist    bool   `json:"plist"`
}

// List reads launchctl and ~/Library/LaunchAgents. Only com.bifrost.* labels.
func List() ([]Service, error) {
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("launchctl list: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home: %w", err)
	}
	matches, _ := filepath.Glob(filepath.Join(home, "Library", "LaunchAgents", "com.bifrost.*.plist"))
	var plist bytes.Buffer
	for _, m := range matches {
		plist.WriteString(m)
		plist.WriteByte('\n')
	}
	return Parse(string(out), plist.String()), nil
}

// Parse merges launchctl list text with a plist path listing.
// last_exit is -1 when launchctl did not report a status.
func Parse(listText, plistText string) []Service {
	byLabel := map[string]*Service{}
	for _, line := range strings.Split(listText, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		label := fields[len(fields)-1]
		if !strings.HasPrefix(label, "com.bifrost.") {
			continue
		}
		pid := 0
		running := false
		if fields[0] != "-" {
			n, err := strconv.Atoi(fields[0])
			if err != nil || n <= 0 {
				continue
			}
			pid = n
			running = true
		}
		last := -1
		if n, err := strconv.Atoi(fields[1]); err == nil {
			last = n
		}
		byLabel[label] = &Service{Label: label, PID: pid, LastExit: last, Running: running}
	}
	for _, line := range strings.Split(plistText, "\n") {
		base := filepath.Base(strings.TrimSpace(line))
		if !strings.HasPrefix(base, "com.bifrost.") || !strings.HasSuffix(base, ".plist") {
			continue
		}
		label := strings.TrimSuffix(base, ".plist")
		if s, ok := byLabel[label]; ok {
			s.Plist = true
			continue
		}
		byLabel[label] = &Service{Label: label, LastExit: -1, Plist: true}
	}
	out := make([]Service, 0, len(byLabel))
	for _, s := range byLabel {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}
