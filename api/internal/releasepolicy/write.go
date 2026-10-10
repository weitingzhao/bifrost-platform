package releasepolicy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigMapWriter replaces the data of a ConfigMap in the pipelines
// namespace, creating it when it is missing.
type ConfigMapWriter interface {
	WriteConfigMap(ctx context.Context, name string, data map[string]string) error
}

// ErrRefused is a request the policy rules turn down (HTTP 409 / 422), as
// opposed to a cluster failure.
var ErrRefused = errors.New("refused")

func refused(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, a...))
}

// Install stores a policy the Owner signed. The signature is the authority:
// the platform verifies it against the compiled-in key before writing, refuses
// an expired one, and refuses one signed before the policy already in place,
// so an older, wider policy cannot be put back.
func (e *Engine) Install(ctx context.Context, policyText, sig string) (Status, error) {
	if e.d.Writer == nil {
		return Status{}, errors.New("no ConfigMap writer")
	}
	now := e.d.Now()
	ev := evaluatePolicy(map[string]string{"policy.yaml": policyText, "policy.sig": sig}, true, nil, e.d.Anchor, now)
	if !ev.valid {
		return Status{}, refused("%s", strings.Join(ev.reasons, "; "))
	}
	cur, found, err := e.read(ctx, PolicyConfigMap)
	if err != nil {
		return Status{}, fmt.Errorf("read the installed policy: %w", err)
	}
	if found {
		if old := evaluatePolicy(cur, true, nil, e.d.Anchor, now); old.policy != nil && !old.signedAt.IsZero() {
			switch {
			case old.policy.PolicyID == ev.policy.PolicyID && cur["policy.yaml"] == policyText:
			case ev.signedAt.Before(old.signedAt):
				return Status{}, refused("policy %s was signed at %s, before the installed %s (%s)",
					ev.policy.PolicyID, ev.policy.SignedAt, old.policy.PolicyID, old.policy.SignedAt)
			}
		}
	}
	if err := e.d.Writer.WriteConfigMap(ctx, PolicyConfigMap, map[string]string{
		"policy.yaml": policyText,
		"policy.sig":  sig,
	}); err != nil {
		return Status{}, err
	}
	return e.Status(ctx), nil
}

// Freeze stops releases. Anyone with the operator role may set it; it only
// tightens.
func (e *Engine) Freeze(ctx context.Context, who, reason string) (Status, error) {
	if e.d.Writer == nil {
		return Status{}, errors.New("no ConfigMap writer")
	}
	who, reason = strings.TrimSpace(who), strings.TrimSpace(reason)
	if who == "" || reason == "" {
		return Status{}, refused("freeze needs who and reason")
	}
	at := e.d.Now().UTC().Truncate(time.Second)
	if err := e.d.Writer.WriteConfigMap(ctx, FreezeConfigMap, map[string]string{
		"frozen":    "true",
		"frozen_at": at.Format(time.RFC3339),
		"who":       who,
		"reason":    reason,
	}); err != nil {
		return Status{}, err
	}
	e.mu.Lock()
	if seen, err := e.loadSeenLocked(); err == nil && at.After(seen) {
		_ = e.storeSeenLocked(at)
	}
	e.mu.Unlock()
	return e.Status(ctx), nil
}

// Unfreeze lifts the current freeze with an Owner signature over
// "unfreeze frozen_at=<the freeze's frozen_at> ..." in the unfreeze
// namespace. The text must name the freeze in place, so a signature for an
// earlier freeze cannot lift a later one.
func (e *Engine) Unfreeze(ctx context.Context, text, sig string) (Status, error) {
	if e.d.Writer == nil {
		return Status{}, errors.New("no ConfigMap writer")
	}
	cur, found, err := e.read(ctx, FreezeConfigMap)
	if err != nil {
		return Status{}, fmt.Errorf("read the freeze: %w", err)
	}
	if !found || !strings.EqualFold(strings.TrimSpace(cur["frozen"]), "true") {
		return Status{}, refused("releases are not frozen")
	}
	at := strings.TrimSpace(cur["frozen_at"])
	if at == "" {
		return Status{}, refused("the freeze has no frozen_at to sign against; set a new freeze first")
	}
	if !strings.HasPrefix(text, "unfreeze frozen_at="+at+" ") {
		return Status{}, refused("the signed text must start with %q", "unfreeze frozen_at="+at+" ")
	}
	if err := verifyAnchored([]byte(sig), []byte(text), NamespaceUnfreeze, e.d.Anchor); err != nil {
		return Status{}, refused("unfreeze signature: %v", err)
	}
	if err := e.d.Writer.WriteConfigMap(ctx, FreezeConfigMap, map[string]string{
		"frozen":       "false",
		"frozen_at":    at,
		"unfreeze.txt": text,
		"unfreeze.sig": sig,
	}); err != nil {
		return Status{}, err
	}
	return e.Status(ctx), nil
}

// PolicyID reads policy_id from a policy body for audit lines. It does not verify.
func PolicyID(policyText string) string {
	var p struct {
		PolicyID string `yaml:"policy_id"`
	}
	if yaml.Unmarshal([]byte(policyText), &p) != nil {
		return ""
	}
	return p.PolicyID
}
