package agentgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TrustOverrideStore holds the Owner's per-skill trust levels. Read and write
// errors are returned, never swallowed: a grant (or a demotion) that silently
// fails to land is the defect this interface exists to prevent (TD-229).
type TrustOverrideStore interface {
	List(ctx context.Context) (map[string]TrustOverride, error)
	Put(ctx context.Context, o TrustOverride) error
	// Location names where the overrides live, so the Console can say which
	// platform instance it is editing.
	Location() string
}

const serviceAccountNSFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// TrustOverrideNamespace is where the ConfigMap store (internal/trustoverrides) lives:
// PLATFORM_GOVERNANCE_NAMESPACE, else — in-cluster — the pod's own namespace.
// Empty outside the cluster without the override, which selects the file store.
func TrustOverrideNamespace() string {
	if ns := strings.TrimSpace(os.Getenv("PLATFORM_GOVERNANCE_NAMESPACE")); ns != "" {
		return ns
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return ""
	}
	raw, err := os.ReadFile(serviceAccountNSFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// NewTrustOverrideStore is the file store for a platform-api outside the
// cluster: PLATFORM_GOVERNANCE_DIR, else PLATFORM_PROJECT_ROOT/agent/governance,
// else PLATFORM_DATA_DIR/governance, else ./data/governance. Never under $HOME:
// a path the operator cannot see from the Console is how the 09-07 grant was lost.
func NewTrustOverrideStore() TrustOverrideStore {
	dir := strings.TrimSpace(os.Getenv("PLATFORM_GOVERNANCE_DIR"))
	if dir == "" {
		if root := strings.TrimSpace(os.Getenv("PLATFORM_PROJECT_ROOT")); root != "" {
			dir = filepath.Join(root, "agent", "governance")
		} else if data := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR")); data != "" {
			dir = filepath.Join(data, "governance")
		} else {
			dir = filepath.Join("data", "governance")
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return &fileTrustOverrides{path: filepath.Join(dir, "trust_overrides.json")}
}

type fileTrustOverrides struct {
	mu   sync.Mutex
	path string
}

func (s *fileTrustOverrides) Location() string { return "file " + s.path }

func (s *fileTrustOverrides) read() (map[string]TrustOverride, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]TrustOverride{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read trust overrides %s: %w", s.path, err)
	}
	return DecodeTrustOverrides(raw, s.path)
}

func (s *fileTrustOverrides) List(context.Context) (map[string]TrustOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *fileTrustOverrides) Put(_ context.Context, o TrustOverride) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read()
	if err != nil {
		return err
	}
	all[o.SkillID] = StampTrustOverride(o)
	raw, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("write trust overrides: %w", err)
	}
	if err := os.WriteFile(s.path, raw, 0o644); err != nil {
		return fmt.Errorf("write trust overrides: %w", err)
	}
	return nil
}

// DecodeTrustOverrides parses a stored overrides document; empty is no overrides.
func DecodeTrustOverrides(raw []byte, where string) (map[string]TrustOverride, error) {
	out := map[string]TrustOverride{}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse trust overrides in %s: %w", where, err)
	}
	if out == nil {
		out = map[string]TrustOverride{}
	}
	return out, nil
}

// StampTrustOverride fills AppliedAt when the caller left it zero.
func StampTrustOverride(o TrustOverride) TrustOverride {
	if o.AppliedAt.IsZero() {
		o.AppliedAt = time.Now().UTC()
	}
	return o
}
