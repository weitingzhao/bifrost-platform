package releasepolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// CandidateKey is where a policy that waits for a signature is kept: a key
// of the policy ConfigMap itself, beside the signed policy.yaml and
// policy.sig, which it leaves as they are. Nothing judges by it.
const CandidateKey = "candidate.yaml"

// Candidate is a policy text that waits for a signature.
type Candidate struct {
	PolicyID  string `json:"policy_id"`
	SHA256    string `json:"sha256"`
	SignedAt  string `json:"signed_at,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func candidateOf(text string) *Candidate {
	if text == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(text))
	c := &Candidate{SHA256: hex.EncodeToString(sum[:])}
	var p Policy
	if yaml.Unmarshal([]byte(text), &p) == nil {
		c.PolicyID, c.SignedAt, c.ExpiresAt = p.PolicyID, p.SignedAt, p.ExpiresAt
	}
	return c
}

// Stage keeps a policy text for the Owner to sign from where the signing key
// is (TD-576: a card he approves with his one-time code signs the bytes whose
// sha256 the card names, with the key in the cluster). Staging gives the text
// no authority: the signed policy stays in force until a signature over the
// candidate is put in its place. A text that could not become a valid policy
// is refused here, and so is one dated before the installed policy, which
// Install would refuse and the card's command does not go through Install.
func (e *Engine) Stage(ctx context.Context, policyText string) (Status, error) {
	if e.d.Writer == nil {
		return Status{}, errors.New("no ConfigMap writer")
	}
	now := e.d.Now()
	var p Policy
	if err := yaml.Unmarshal([]byte(policyText), &p); err != nil {
		return Status{}, refused("the candidate does not parse: %v", err)
	}
	signed, err1 := time.Parse(time.RFC3339, p.SignedAt)
	expires, err2 := time.Parse(time.RFC3339, p.ExpiresAt)
	switch {
	case strings.TrimSpace(p.PolicyID) == "":
		return Status{}, refused("the candidate has no policy_id")
	case p.Version != 1:
		return Status{}, refused("candidate version %d is not 1", p.Version)
	case err1 != nil || err2 != nil:
		return Status{}, refused("the candidate has no signed_at / expires_at")
	case signed.After(now.Add(clockSkew)):
		return Status{}, refused("candidate signed_at %s is in the future", p.SignedAt)
	case !expires.After(now):
		return Status{}, refused("candidate %s expired at %s", p.PolicyID, p.ExpiresAt)
	case len(p.Allow) == 0:
		return Status{}, refused("the candidate allows no pipeline")
	}
	cur, _, err := e.read(ctx, PolicyConfigMap)
	if err != nil {
		return Status{}, fmt.Errorf("read the installed policy: %w", err)
	}
	if old := evaluatePolicy(cur, true, nil, e.d.Anchor, now); old.policy != nil && !old.signedAt.IsZero() && signed.Before(old.signedAt) {
		return Status{}, refused("candidate %s is dated %s, before the installed %s (%s)", p.PolicyID, p.SignedAt, old.policy.PolicyID, old.policy.SignedAt)
	}
	data := map[string]string{}
	for k, v := range cur {
		data[k] = v
	}
	data[CandidateKey] = policyText
	if err := e.d.Writer.WriteConfigMap(ctx, PolicyConfigMap, data); err != nil {
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
