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
//
// resourceVersion is the version from the read that authorized the write.
// Empty means the read found no object, so the writer creates one. A write
// whose version is no longer current returns ErrConflict and leaves the
// object as it is.
type ConfigMapWriter interface {
	WriteConfigMap(ctx context.Context, name string, data map[string]string, resourceVersion string) error
}

// ErrRefused is a request the policy rules turn down (HTTP 409 / 422), as
// opposed to a cluster failure.
var ErrRefused = errors.New("refused")

// ErrConflict is a write that lost a race with another update of the same
// object. The caller verified a different version. Nothing was written.
var ErrConflict = errors.New("configmap conflict")

func refused(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, a...))
}

// unfreezeChanged is the refusal when the freeze object moved between the
// read that verified the signature and the write that would lift it.
const unfreezeChanged = "the freeze changed while the unfreeze was being checked; read the status and sign again"

// installChanged is the refusal when the policy object moved between the
// read that compared signed_at and the write. Overwriting a policy that was
// signed later could put an older, wider one back.
const installChanged = "the installed policy changed while this policy was being checked; read the status and submit it again"

// Install stores a policy the Owner signed. The signature is the authority:
// the platform verifies it against the compiled-in key before writing, refuses
// an expired one, and refuses one signed before the policy already in place,
// so an older, wider policy cannot be put back. The write carries the
// resourceVersion of that read. A conflict writes nothing: the policy in
// place is not the one this signature was compared with.
func (e *Engine) Install(ctx context.Context, policyText, sig string) (Status, error) {
	if e.d.Writer == nil {
		return Status{}, errors.New("no ConfigMap writer")
	}
	now := e.d.Now()
	ev := evaluatePolicy(map[string]string{"policy.yaml": policyText, "policy.sig": sig}, true, nil, e.d.Anchor, now)
	if !ev.valid {
		return Status{}, refused("%s", strings.Join(ev.reasons, "; "))
	}
	cur, rv, found, err := e.read(ctx, PolicyConfigMap)
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
	} else {
		rv = ""
	}
	if err := e.d.Writer.WriteConfigMap(ctx, PolicyConfigMap, map[string]string{
		"policy.yaml": policyText,
		"policy.sig":  sig,
	}, rv); err != nil {
		if errors.Is(err, ErrConflict) {
			return Status{}, refused("%s", installChanged)
		}
		return Status{}, err
	}
	return e.Status(ctx), nil
}

// freezeRefusedNoAnchor is the text Freeze returns, via refused, when no
// trust anchor is compiled in. A freeze that no signature could lift is not
// offered.
const freezeRefusedNoAnchor = `freeze refused: platform-api has no trust anchor, so no signature could lift it. Generate the release owner key and compile its fingerprint in first (docs/RELEASE.md, "The release owner key").`

// Freeze stops releases. Anyone with the operator role may set it; it only
// tightens. It is refused while the anchor is empty, before any write and
// before the remembered newest freeze changes: until a signature could lift
// it, the switch is not offered.
//
// Each freeze gets a new 128-bit id from crypto/rand. The id is not a counter,
// not a clock, and not derived from the freeze this process remembers, so two
// freezes cannot share one after a reset or across two platforms. The write
// carries the resourceVersion of the read. A conflict writes nothing and does
// not retry: the id that lost the race was never published. A freeze that
// overwrote an unfreeze would only tighten; the condition is what keeps one
// id from being published onto an object that changed.
func (e *Engine) Freeze(ctx context.Context, who, reason string) (Status, error) {
	if strings.TrimSpace(e.d.Anchor) == "" {
		return Status{}, refused("%s", freezeRefusedNoAnchor)
	}
	if e.d.Writer == nil {
		return Status{}, errors.New("no ConfigMap writer")
	}
	who, reason = strings.TrimSpace(who), strings.TrimSpace(reason)
	if who == "" || reason == "" {
		return Status{}, refused("freeze needs who and reason")
	}
	at := e.d.Now().UTC().Truncate(time.Second)
	id, err := newFreezeID()
	if err != nil {
		return Status{}, err
	}
	_, rv, found, err := e.read(ctx, FreezeConfigMap)
	if err != nil {
		return Status{}, fmt.Errorf("read the freeze: %w", err)
	}
	if !found {
		rv = ""
	}
	err = e.d.Writer.WriteConfigMap(ctx, FreezeConfigMap, map[string]string{
		"frozen":    "true",
		"frozen_at": at.Format(time.RFC3339),
		"freeze_id": id,
		"who":       who,
		"reason":    reason,
	}, rv)
	if errors.Is(err, ErrConflict) {
		return Status{}, refused("the freeze changed while this freeze was being written; try again")
	}
	if err != nil {
		return Status{}, err
	}
	e.mu.Lock()
	rerr := e.rememberLocked(seenSnap{id: id, at: at})
	e.mu.Unlock()
	if rerr != nil {
		return Status{}, fmt.Errorf("record the freeze: %w", rerr)
	}
	return e.Status(ctx), nil
}

// Unfreeze lifts the current freeze with an Owner signature over
// "unfreeze freeze_id=<that freeze's id> frozen_at=<its frozen_at> ..." in
// the unfreeze namespace. The text must name the freeze in place by id.
// frozen_at stays in the text for the person signing it. A signature that
// names only frozen_at, the form used before freeze_id existed, is refused:
// no such signature has been made. The write uses the resourceVersion of the
// object that was verified. If that object changed, nothing is written.
func (e *Engine) Unfreeze(ctx context.Context, text, sig string) (Status, error) {
	if e.d.Writer == nil {
		return Status{}, errors.New("no ConfigMap writer")
	}
	cur, rv, found, err := e.read(ctx, FreezeConfigMap)
	if err != nil {
		return Status{}, fmt.Errorf("read the freeze: %w", err)
	}
	if !found || !strings.EqualFold(strings.TrimSpace(cur["frozen"]), "true") {
		return Status{}, refused("releases are not frozen")
	}
	id := strings.TrimSpace(cur["freeze_id"])
	at := strings.TrimSpace(cur["frozen_at"])
	if !wellFormedFreezeID(id) {
		return Status{}, refused("%s", unidentifiedFreeze)
	}
	if at == "" {
		return Status{}, refused("the freeze has no frozen_at to sign against; set a new freeze first")
	}
	parsedAt, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return Status{}, refused("frozen_at %q is not a timestamp", at)
	}
	e.mu.Lock()
	block, berr := e.blocksLaterLocked(id)
	if berr != nil {
		e.mu.Unlock()
		return Status{}, fmt.Errorf("read the freeze history: %w", berr)
	}
	if block {
		e.mu.Unlock()
		return Status{}, refused("%s", laterFreezeGone)
	}
	if rerr := e.rememberLocked(seenSnap{id: id, at: parsedAt}); rerr != nil {
		e.mu.Unlock()
		return Status{}, fmt.Errorf("record the freeze: %w", rerr)
	}
	e.mu.Unlock()
	if named, ok := signedFreezeID(text); ok && named != id {
		return Status{}, refused("the unfreeze names %s, not %s", named, id)
	}
	prefix := UnfreezePrefix(id, at)
	if !unfreezeNames(text, id, at) {
		return Status{}, refused("the signed text must start with %q", prefix)
	}
	if err := verifyAnchored([]byte(sig), []byte(text), NamespaceUnfreeze, e.d.Anchor); err != nil {
		return Status{}, refused("unfreeze signature: %v", err)
	}
	if err := e.d.Writer.WriteConfigMap(ctx, FreezeConfigMap, map[string]string{
		"frozen":       "false",
		"frozen_at":    at,
		"freeze_id":    id,
		"unfreeze.txt": text,
		"unfreeze.sig": sig,
	}, rv); err != nil {
		if errors.Is(err, ErrConflict) {
			return Status{}, refused("%s", unfreezeChanged)
		}
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
