// Package ownerexec is the cluster-side runner for owner_run_command cards.
//
// The Console sends it one request per card: the card id, the command
// fingerprint the Owner was shown, a one-time code from the Owner's
// authenticator, and the admin bearer the page already holds. It runs the
// command only when all three hold: the code is valid and unused, the card is
// approved (it approves a pending card with that bearer, on the Owner's
// click), and the claimed command hashes to the fingerprint.
//
// Threat note: the admin bearer is readable on the Owner's Mac by anything
// running there, so it is not the lock. The one-time code is. Nothing here may
// run on the bearer alone, and neither the bearer nor the code is ever logged
// or written to disk.
package ownerexec

import (
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- RFC 6238 authenticator apps use HMAC-SHA1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// Step and Digits are what authenticator apps assume for an otpauth URI
	// that does not say otherwise.
	Step   = 30 * time.Second
	Digits = 6

	// Warmup: lastStep lives in memory only, so after a restart a code used
	// just before it could be replayed. No code is accepted until every step
	// that could have been used before the restart is out of the window.
	Warmup = 90 * time.Second

	maxFailures = 5
	lockFor     = 15 * time.Minute
)

var (
	ErrWarmup   = errors.New("just started: codes are accepted 90 seconds after start")
	ErrLocked   = errors.New("too many wrong codes: locked for 15 minutes")
	ErrBadCode  = errors.New("wrong code")
	ErrReplayed = errors.New("this code was already used")
)

// DecodeSeed reads a base32 seed as authenticator apps take it (case and
// padding ignored). At least 16 bytes.
func DecodeSeed(s string) ([]byte, error) {
	s = strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "", "\n", "", "\r", "").Replace(s))
	seed, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		return nil, errors.New("seed is not base32")
	}
	if len(seed) < 16 {
		return nil, errors.New("seed is shorter than 16 bytes")
	}
	return seed, nil
}

// EncodeSeed is the base32 form an authenticator app accepts by hand.
func EncodeSeed(seed []byte) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(seed)
}

// CodeAt is the RFC 6238 code for one 30-second step.
func CodeAt(seed []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, seed)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", Digits, bin%1000000)
}

func stepOf(t time.Time) int64 { return t.Unix() / int64(Step/time.Second) }

// Verifier accepts each time step at most once and locks after repeated
// wrong codes. One per process; the Deployment runs one replica (Recreate).
type Verifier struct {
	seed    []byte
	started time.Time

	mu       sync.Mutex
	lastStep int64
	failures int
	lockedTo time.Time
}

func NewVerifier(seed []byte, started time.Time) *Verifier {
	return &Verifier{seed: seed, started: started}
}

// Verify consumes code. A step at or before the last accepted one is refused,
// so the same code never passes twice and an older code never passes after a
// newer one.
func (v *Verifier) Verify(code string, now time.Time) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if now.Sub(v.started) < Warmup {
		return ErrWarmup
	}
	if now.Before(v.lockedTo) {
		return ErrLocked
	}
	code = strings.TrimSpace(code)
	cur := stepOf(now)
	matched := int64(-1)
	// Constant-time over the whole window: no early exit on a match.
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		if len(code) == Digits && subtle.ConstantTimeCompare([]byte(CodeAt(v.seed, s)), []byte(code)) == 1 {
			matched = s
		}
	}
	if matched < 0 {
		v.failures++
		if v.failures >= maxFailures {
			v.failures = 0
			v.lockedTo = now.Add(lockFor)
		}
		return ErrBadCode
	}
	if matched <= v.lastStep {
		return ErrReplayed
	}
	v.lastStep = matched
	v.failures = 0
	return nil
}
