package landing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"time"
)

const S5Version = "1.3"

// s5Kind: the records v1.3 may carry. failed is the operation's end with an
// S5 code (B2: a deterministic S5 refusal releases the repository's slot).
func s5Kind(r Record) bool {
	switch r.String("kind") {
	case "s5_required", "s5_key", "s5_run", "s5_failed":
		return true
	case "failed":
		return S5Code(r.String("why"))
	}
	return false
}

var s5CodePattern = regexp.MustCompile(`^S5_[A-Z][A-Z_]{1,60}$`)

// S5Code is a stable refusal code from the §5 table's namespace.
func S5Code(why string) bool { return s5CodePattern.MatchString(why) }

// S5 infrastructure retries (§5.1 "基础设施重试只能在既有次数／期限内显式开新
// generation"): a run the release freeze check refused never reached the
// mandatory tasks; the operation may open the next generation with the
// same material at most S5InfraRetries times, each after S5InfraRetryAfter.
// The root's own deadline still ends it first.
const S5InfraRetries = 3

func S5InfraRetryAfter(done int) time.Duration { return 15 * time.Minute << done }

type S5State struct {
	Required                bool
	Key                     S5CIKey
	KeyDigest, UID, Failure string
	// KeyAt is when the current generation was fixed; InfraRetries counts
	// the operation's generations opened by an infrastructure retry.
	KeyAt        time.Time
	InfraRetries int
}

// S5 events preserve the operation's phase. The first key marks the
// requirement atomically; a separate claim marker closes pre-key recovery.
func parseS5Record(r Record) error {
	kind := r.String("kind")
	if kind == "s5_required" {
		return nil
	}
	var key S5CIKey
	if kind == "s5_key" {
		b, e := json.Marshal(r["key"])
		if e != nil {
			return e
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if e = dec.Decode(&key); e != nil || !key.Valid() || key.Op != r.String("op") {
			return fmt.Errorf("S5_BINDING: invalid key")
		}
		d, _ := key.Digest()
		if d != r.String("key_digest") {
			return fmt.Errorf("S5_BINDING: key digest")
		}
	}
	return nil
}

func (s *State) applyS5(e Entry) error {
	r := e.Record
	op := r.String("op")
	o, ok := s.Operations[op]
	if !ok || o.Terminal() || o.AtMain() || !slices.Contains([]string{"claimed", "prepared", "branch_intent", "branch_pushed", "parked"}, o.Phase) {
		return fmt.Errorf("S5_BINDING: operation phase")
	}
	switch r.String("kind") {
	case "s5_required":
		if o.S5.Required || o.Phase != "claimed" {
			return fmt.Errorf("S5_BINDING: duplicate requirement")
		}
		o.S5.Required = true
	case "s5_key":
		var key S5CIKey
		b, _ := json.Marshal(r["key"])
		if err := json.Unmarshal(b, &key); err != nil {
			return err
		}
		if o.R == "" || key.R != o.R || key.M0 != o.M0 || key.A != o.A || key.Task != o.Task || key.Attempt != o.Attempt || (key.Env == "stg") != o.DryRun {
			return fmt.Errorf("S5_BINDING: operation key")
		}
		if o.S5.Key.Generation == math.MaxInt64 || key.Generation != o.S5.Key.Generation+1 {
			return fmt.Errorf("S5_GENERATION_STALE")
		}
		// A new key must change material, never silently rerun a failed
		// identity. The one exception is the explicit infrastructure retry:
		// the same material, after a bound run that was never failed, within
		// the retry count and backoff.
		prior := o.S5.Key
		prior.Generation = key.Generation
		retries := o.S5.InfraRetries
		if r.String("retry") == "infra" {
			if prior != key || o.S5.UID == "" || o.S5.Failure != "" || retries >= S5InfraRetries || e.At.Before(o.S5.KeyAt.Add(S5InfraRetryAfter(retries))) {
				return fmt.Errorf("S5_GENERATION_STALE: infrastructure retry")
			}
			retries++
		} else if prior == key {
			return fmt.Errorf("S5_BINDING: unchanged generation")
		}
		o.S5 = S5State{Required: true, Key: key, KeyDigest: r.String("key_digest"), KeyAt: e.At, InfraRetries: retries}
		o.CI = nil
	case "s5_run":
		if !o.S5.Required || o.S5.KeyDigest == "" || r.String("key_digest") != o.S5.KeyDigest || o.S5.UID != "" || o.S5.Failure != "" {
			return fmt.Errorf("S5_CI_IDENTITY")
		}
		o.S5.UID = r.String("uid")
	case "s5_failed":
		if o.S5.KeyDigest == "" || r.String("key_digest") != o.S5.KeyDigest || o.S5.Failure != "" {
			return fmt.Errorf("S5_GENERATION_STALE")
		}
		o.S5.Failure = r.String("why")
		o.CI = nil
	}
	o.LastAt = e.At
	s.Operations[op] = o
	return nil
}
