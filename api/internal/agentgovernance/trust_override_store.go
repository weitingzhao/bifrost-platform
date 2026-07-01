package agentgovernance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TrustOverride persists Owner actuation level policy for a skill (Flight Director).
type TrustOverride struct {
	SkillID   string    `json:"skill_id"`
	Level     string    `json:"level"`
	Reason    string    `json:"reason,omitempty"`
	AppliedBy string    `json:"applied_by,omitempty"`
	AppliedAt time.Time `json:"applied_at"`
}

type TrustOverrideStore struct {
	mu   sync.Mutex
	path string
}

func NewTrustOverrideStore() *TrustOverrideStore {
	dir := os.Getenv("PLATFORM_GOVERNANCE_DIR")
	if dir == "" {
		if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
			dir = filepath.Join(root, "agent", "governance")
		} else {
			dir = filepath.Join(os.Getenv("HOME"), ".bifrost-platform", "governance")
		}
	}
	_ = os.MkdirAll(dir, 0o755)
	return &TrustOverrideStore{path: filepath.Join(dir, "trust_overrides.json")}
}

func (s *TrustOverrideStore) List() map[string]TrustOverride {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return map[string]TrustOverride{}
	}
	var out map[string]TrustOverride
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return map[string]TrustOverride{}
	}
	return out
}

func (s *TrustOverrideStore) Put(o TrustOverride) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := map[string]TrustOverride{}
	if raw, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(raw, &all)
	}
	if all == nil {
		all = map[string]TrustOverride{}
	}
	if o.AppliedAt.IsZero() {
		o.AppliedAt = time.Now().UTC()
	}
	all[o.SkillID] = o
	raw, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, raw, 0o644)
}
