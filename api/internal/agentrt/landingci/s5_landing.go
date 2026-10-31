package landingci

import (
	"context"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// These three adapters are the landing store/controller's S5 callbacks
// (store.LandingSource.S5Policy / S5CI, controller.DrillSources.EnsureS5CI).
// The server only wires them; all trust decisions stay in this package.

// S5Policy returns the deployment-owned S5 material from the embedded trust
// root. UNBUILT image, unpinned materials or a profile that is not the
// reviewed manifest are errors: the store then leaves Facts.S5Policy zero
// and every S5 gate refuses, while non-S5 lanes never call this.
func S5Policy(context.Context) (landing.S5Policy, error) {
	c, err := deployedS5Contract()
	if err != nil {
		return landing.S5Policy{}, err
	}
	return c.policy()
}

// EnsureLandingS5CI starts (or recovers) the run for the persisted key and
// returns only the server-assigned UID, after checking the returned run is
// exactly the key's run name, full digest and generation.
func (c *Client) EnsureLandingS5CI(ctx context.Context, key landing.S5CIKey) (string, error) {
	contract, err := deployedS5Contract()
	if err != nil {
		return "", err
	}
	return c.ensureLandingS5CI(ctx, key, contract)
}
func (c *Client) ensureLandingS5CI(ctx context.Context, key landing.S5CIKey, contract s5Contract) (string, error) {
	id, err := c.ensureS5(ctx, key, contract)
	if err != nil {
		return "", err
	}
	if !s5IdentityForKey(key, id) {
		return "", fmt.Errorf("S5_CI_IDENTITY: run identity does not match the persisted key")
	}
	return id.UID, nil
}

// LandingS5CI reads the run bound to the persisted key and UID. The saved
// identity is derived from the key itself plus expectedUID (the UID line B
// bound after EnsureLandingS5CI); a missing UID is never reconstructed.
func (c *Client) LandingS5CI(ctx context.Context, key landing.S5CIKey, expectedUID string) (landing.S5CIResult, error) {
	contract, err := deployedS5Contract()
	if err != nil {
		return landing.S5CIResult{}, err
	}
	return c.landingS5CI(ctx, key, expectedUID, contract)
}
func (c *Client) landingS5CI(ctx context.Context, key landing.S5CIKey, expectedUID string, contract s5Contract) (landing.S5CIResult, error) {
	name, err := key.RunName()
	if err != nil {
		return landing.S5CIResult{}, err
	}
	digest, err := key.Digest()
	if err != nil {
		return landing.S5CIResult{}, err
	}
	saved := S5RunIdentity{Run: name, UID: expectedUID, KeyDigest: digest, Generation: key.Generation}
	ev, err := c.readS5(ctx, key, saved, contract)
	if err != nil {
		return landing.S5CIResult{}, err
	}
	return s5LandingResult(key, saved, ev), nil
}

func s5IdentityForKey(key landing.S5CIKey, id S5RunIdentity) bool {
	name, e1 := key.RunName()
	digest, e2 := key.Digest()
	return e1 == nil && e2 == nil && id.UID != "" && id.Run == name && id.KeyDigest == digest && id.Generation == key.Generation
}

// s5LandingResult converts transport evidence into the landing result.
// IdentityVerified is true ONLY for a Succeeded read with no reason whose
// content decodes strictly and hashes to the reported evidence digest:
// readS5 has then checked the run, resolved pipeline/tasks, image, mandatory
// task results and every key binding. Everything else leaves it false; a
// failure keeps the reader's own reason, which landing reports as is.
func s5LandingResult(key landing.S5CIKey, saved S5RunIdentity, ev S5Evidence) landing.S5CIResult {
	out := landing.S5CIResult{Key: key, Run: saved.Run, UID: saved.UID, KeyDigest: saved.KeyDigest, CreatedAt: ev.CreatedAt}
	fail := func(reason string) landing.S5CIResult {
		if reason == "" {
			reason = "S5_CI_IDENTITY"
		}
		out.Status, out.Reason = "Failed", reason
		return out
	}
	if ev.Identity != saved || !s5IdentityForKey(key, saved) {
		return fail("S5_CI_IDENTITY")
	}
	switch {
	case ev.Status == "Succeeded" && ev.Reason == "":
		var content landing.S5ContentEvidence
		if len(ev.Content) == 0 || ev.Digest == "" || s5Hash(ev.Content) != ev.Digest || decodeS5(ev.Content, &content) != nil {
			return fail("S5_CI_IDENTITY")
		}
		if ev.CreatedAt.IsZero() || ev.CompletedAt.IsZero() || ev.CompletedAt.Before(ev.CreatedAt) {
			return fail("S5_CI_IDENTITY")
		}
		out.Status, out.CompletedAt, out.IdentityVerified, out.Evidence = "Succeeded", ev.CompletedAt, true, &content
		return out
	case ev.Status == "Running" && ev.Reason == "S5_EVIDENCE_MISSING":
		out.Status = "Running"
		return out
	case ev.Status == "Unknown" && ev.Reason == "S5_EVIDENCE_MISSING":
		// Not found yet, or not terminal: wait (never authorizes).
		out.Status = "Missing"
		return out
	case ev.Status == "Succeeded":
		// A reason on a "Succeeded" status is contradictory: never adopt.
		return fail("S5_CI_IDENTITY")
	default:
		r := fail(ev.Reason)
		// Only a failed run the reader attributed to the freeze check.
		r.Retryable = ev.Retryable && ev.Status == "Failed" && ev.Reason == "S5_EVIDENCE_MISSING"
		return r
	}
}
