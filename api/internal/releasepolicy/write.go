package releasepolicy

import (
	"context"
	"errors"
	"fmt"
	"math"
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
// Each freeze gets a freeze_id strictly greater than any id this platform has
// seen or that is already on the object. The id is chosen from that read and
// published in the same write, conditional on the object's resourceVersion.
// A conflict allocates again and does not keep the id it lost, so two freezes
// cannot share one. A freeze that overwrote an unfreeze would only tighten;
// publishing one id onto two freezes would not, which is why the write is
// conditional anyway.
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
	for attempt := 0; attempt < 5; attempt++ {
		id, rv, err := e.allocateFreezeID(ctx)
		if err != nil {
			return Status{}, err
		}
		err = e.d.Writer.WriteConfigMap(ctx, FreezeConfigMap, map[string]string{
			"frozen":    "true",
			"frozen_at": at.Format(time.RFC3339),
			"freeze_id": id,
			"who":       who,
			"reason":    reason,
		}, rv)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return Status{}, err
		}
		n, _ := parseFreezeID(id)
		e.noteFreeze(seenSnap{at: at, id: n})
		return e.Status(ctx), nil
	}
	return Status{}, refused("the freeze changed while a new freeze id was being allocated; try again")
}

// allocateFreezeID returns the next freeze id and the resourceVersion the
// write must match. The id is max(remembered, id on the object) + 1, so it
// is strictly greater than either. A missing object returns an empty version,
// which creates it.
func (e *Engine) allocateFreezeID(ctx context.Context) (string, string, error) {
	data, rv, found, err := e.read(ctx, FreezeConfigMap)
	if err != nil {
		return "", "", fmt.Errorf("read the freeze: %w", err)
	}
	e.mu.Lock()
	seen, serr := e.loadSeenLocked()
	e.mu.Unlock()
	if serr != nil {
		return "", "", fmt.Errorf("read the freeze history: %w", serr)
	}
	base := seen.id
	if found {
		if id, ok := parseFreezeID(data["freeze_id"]); ok && id > base {
			base = id
		}
	} else {
		rv = ""
	}
	if base == math.MaxUint64 {
		return "", "", refused("the freeze counter is exhausted")
	}
	return fmt.Sprintf("%d", base+1), rv, nil
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
	if _, ok := parseFreezeID(id); !ok {
		return Status{}, refused("the freeze has no identifier to sign against; set a new freeze first")
	}
	if at == "" {
		return Status{}, refused("the freeze has no frozen_at to sign against; set a new freeze first")
	}
	prefix := UnfreezePrefix(id, at)
	if !strings.HasPrefix(text, prefix) {
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
