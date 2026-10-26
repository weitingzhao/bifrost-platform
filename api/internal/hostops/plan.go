package hostops

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

type Target struct {
	Host        string `json:"host"`
	Environment string `json:"environment"`
	HostKey     string `json:"host_key"`
	OS          string `json:"os"`
	Generation  uint64 `json:"generation"`
}
type Rollback struct {
	Action          string            `json:"action"`
	Params          map[string]string `json:"params"`
	ArtifactDigests []string          `json:"artifact_digests"`
	Deadline        string            `json:"deadline"`
	TimeoutS        int               `json:"timeout_s"`
	Effects         []Effect          `json:"effects"`
}
type Plan struct {
	SchemaVersion      int               `json:"schema_version"`
	Action             string            `json:"action"`
	Params             map[string]string `json:"params"`
	Target             Target            `json:"target"`
	BeforeDigest       string            `json:"before_digest"`
	ArtifactDigests    []string          `json:"artifact_digests"`
	InventoryDigest    string            `json:"inventory_digest"`
	CatalogDigest      string            `json:"catalog_digest"`
	HandlerDigest      string            `json:"handler_digest"`
	ExecutionID        string            `json:"execution_id"`
	ApprovalID         string            `json:"approval_id"`
	Nonce              string            `json:"nonce"`
	EffectiveAt        string            `json:"effective_at"`
	ExpiresAt          string            `json:"expires_at"`
	ExecutorID         string            `json:"executor_id"`
	FreezeEpoch        uint64            `json:"freeze_epoch"`
	RecoveryGeneration uint64            `json:"recovery_generation"`
	FencingGeneration  uint64            `json:"fencing_generation"`
	Rollback           Rollback          `json:"rollback"`
	Locks              []string          `json:"locks"`
	Effects            []Effect          `json:"effects"`
}

func (p Plan) validate() error {
	if !validParams(p.Action, p.Params) || !nonemptyParams(p.Params) {
		return errors.New("unknown, missing or empty action parameter")
	}
	if !hostFingerprint(p.Target.HostKey) || !contains([]string{"linux", "macos", "ugos"}, p.Target.OS) || !contains([]string{"test", "dev", "stg", "prod"}, p.Target.Environment) {
		return errors.New("invalid target identity")
	}
	if p.SchemaVersion != 1 || p.Action == "" || p.Target.Host == "" || p.Target.Environment == "" || p.Target.HostKey == "" || p.Target.Generation == 0 || p.ExecutorID == "" || p.ExecutionID == "" || p.ApprovalID == "" || len(p.Nonce) < 32 || p.FreezeEpoch == 0 || p.RecoveryGeneration == 0 || p.FencingGeneration == 0 || !setOK(p.Locks, false) || !contains(p.Locks, "host:"+p.Target.Host) || !setOK(p.ArtifactDigests, true) || !setOK(p.Rollback.ArtifactDigests, true) || p.Rollback.Action == "" || p.Rollback.TimeoutS < 1 || len(p.Effects) == 0 || len(p.Rollback.Effects) == 0 {
		return errors.New("incomplete plan")
	}
	if _, _, err := times(p.EffectiveAt, p.ExpiresAt); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, p.Rollback.Deadline); err != nil {
		return err
	}
	digests := append([]string{p.BeforeDigest, p.InventoryDigest, p.CatalogDigest, p.HandlerDigest}, p.ArtifactDigests...)
	for _, d := range append(digests, p.Rollback.ArtifactDigests...) {
		if !validDigest(d) {
			return errors.New("invalid plan digest")
		}
	}
	for _, e := range append(append([]Effect{}, p.Effects...), p.Rollback.Effects...) {
		if !validDigest(e.SourceDigest) || e.Identity == "" || e.Operation == "" {
			return errors.New("invalid effect")
		}
	}
	return nil
}

// CanonicalPlan is protocol v1: encoding/json on the closed Plan struct,
// UTF-8, no whitespace/newline, sorted map keys, HTML escaping enabled.
// Array order is significant. Signers must display this exact immutable
// object and independently calculate the SHA256:<hex> lowercase digest.
func CanonicalPlan(raw []byte) ([]byte, string, error) {
	if !json.Valid(raw) {
		return nil, "", errors.New("plan must be JSON")
	}
	var p Plan
	if err := Decode(raw, &p); err != nil {
		return nil, "", err
	}
	if err := p.validate(); err != nil {
		return nil, "", err
	}
	b := canonical(p)
	return b, Digest(b), nil
}

// PlanContext contains independently observed current state, not claims from
// a queue payload. Effects and lock closure must be reconstructed from actual
// bytes. S3 adapters remain unadmitted until they can supply these proofs.
type PlanContext struct {
	Clock               ClockObservation
	Target              Target
	ExecutorID          string
	ExecutionID         string
	ApprovalID          string
	BeforeDigest        string
	InventoryDigest     string
	HandlerDigest       string
	ArtifactDigests     []string
	Rollback            Rollback
	Effects             []Effect
	Locks               []string
	FencingGeneration   uint64
	State               SafetyState
	Catalog             SafetyCatalog
	PreinstalledActions []string
	Consumptions        []Consumption
}

// VerifyApproval verifies H authorization; it does not consume the nonce or
// execute. It requires canonical bytes and SSHSIG over ASCII plan_hash (no
// newline), not a UI "approved" flag. Boundary checks precede signature use.
func (t Trust) VerifyApproval(raw []byte, signature string, c PlanContext, now time.Time) (Plan, error) {
	var p Plan
	if err := c.Clock.Check(now); err != nil {
		return p, err
	}
	b, hash, err := CanonicalPlan(raw)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(b, raw) {
		return p, errors.New("noncanonical plan")
	}
	if err := Decode(b, &p); err != nil {
		return p, err
	}
	if err := c.Catalog.Boundary(p.Action, "H", append(append([]Effect{}, p.Effects...), p.Rollback.Effects...)); err != nil {
		return p, err
	}
	if !contains(c.PreinstalledActions, p.Action) || c.State.Check(p.Target.Environment, c.State.MaxRuleVersion, c.State.PolicyHead) != nil || !current(p.EffectiveAt, p.ExpiresAt, now) || p.FreezeEpoch != c.State.Epoch || p.RecoveryGeneration != c.State.Generation || p.FencingGeneration != c.FencingGeneration {
		return p, errors.New("H capability, freeze, generation or validity mismatch")
	}
	if p.Target != c.Target || p.ExecutorID != c.ExecutorID || p.ExecutionID != c.ExecutionID || p.ApprovalID != c.ApprovalID || p.BeforeDigest != c.BeforeDigest || p.InventoryDigest != c.InventoryDigest || p.CatalogDigest != c.Catalog.Digest || p.HandlerDigest != c.HandlerDigest || !reflect.DeepEqual(p.ArtifactDigests, c.ArtifactDigests) || !reflect.DeepEqual(p.Rollback, c.Rollback) || !reflect.DeepEqual(p.Effects, c.Effects) || !reflect.DeepEqual(p.Locks, c.Locks) {
		return p, errors.New("plan differs from current identity, bytes, before state or dependency closure")
	}
	for _, record := range c.Consumptions {
		if record.Nonce == p.Nonce || record.ExecutionID == p.ExecutionID {
			return p, errors.New("consumed approval; lookup existing result only")
		}
	}
	if err := t.verify(Signed{Text: hash, Signature: signature}, NamespaceApproval); err != nil {
		return p, err
	}
	return p, nil
}
