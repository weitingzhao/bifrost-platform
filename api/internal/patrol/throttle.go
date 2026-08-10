package patrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	throttleCooldown = 24 * time.Hour
	throttlePrune    = 48 * time.Hour
)

// RestartThrottle prevents the same target from being restarted more than once
// within a 24-hour window. State is persisted to disk.
type RestartThrottle struct {
	mu   sync.Mutex
	path string
	data map[string]time.Time // target → last restart UTC
	now  func() time.Time
}

func NewRestartThrottle(stateDir string, now func() time.Time) (*RestartThrottle, error) {
	if stateDir == "" {
		stateDir = DefaultStateDir()
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	t := &RestartThrottle{
		path: filepath.Join(stateDir, "autopilot-throttle.json"),
		data: map[string]time.Time{},
		now:  now,
	}
	t.load()
	t.prune()
	return t, nil
}

// CanRestart returns true if the target has not been restarted in the last 24h.
func (t *RestartThrottle) CanRestart(target string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	last, ok := t.data[target]
	if !ok {
		return true
	}
	return t.now().Sub(last) >= throttleCooldown
}

// RecordRestart marks the target as just restarted.
func (t *RestartThrottle) RecordRestart(target string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data[target] = t.now().UTC()
	t.saveLocked()
}

func (t *RestartThrottle) load() {
	raw, err := os.ReadFile(t.path)
	if err != nil {
		return
	}
	var stored map[string]string
	if json.Unmarshal(raw, &stored) != nil {
		return
	}
	for k, v := range stored {
		if ts, err := time.Parse(time.RFC3339, v); err == nil {
			t.data[k] = ts
		}
	}
}

func (t *RestartThrottle) prune() {
	now := t.now()
	for k, v := range t.data {
		if now.Sub(v) > throttlePrune {
			delete(t.data, k)
		}
	}
}

func (t *RestartThrottle) saveLocked() {
	t.prune()
	stored := make(map[string]string, len(t.data))
	for k, v := range t.data {
		stored[k] = v.UTC().Format(time.RFC3339)
	}
	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return
	}
	tmp := t.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, t.path)
	}
}
