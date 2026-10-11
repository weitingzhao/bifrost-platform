package releasepolicy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// FreezeProtocol is the freeze-object contract this process implements.
// release.sh refuses to freeze unless the status reports this same number.
// Mixed versions then fail closed instead of wedging every release.
const FreezeProtocol = 2

// freezeIDBytes is the random identity of one freeze. 16 bytes is 128 bits,
// the minimum. It is not a counter and it does not read the clock, so a
// reset, a repeated timestamp, or a second platform writing the same object
// cannot mint an id that already exists.
const freezeIDBytes = 16

// unidentifiedFreeze is the status text when the object says frozen and has
// no well-formed id. There is nothing to sign. The way out is the break-glass.
const unidentifiedFreeze = `this freeze has no identifier, so no signature can lift it; follow docs/RELEASE.md "Lifting a freeze without a signature (break-glass)"`

// laterFreezeGone is the refusal when the object no longer shows the freeze
// this platform remembers as the latest one.
const laterFreezeGone = "a later freeze was seen and its record is gone"

// seenSnap is the latest freeze this process has seen in place, and the
// earlier identities it must not return to.
//
// A well-formed freeze is {id, at}. An object that says frozen without a
// well-formed id is {unidentified, at, seenAt}: seenAt is when this process
// noticed it, because the object itself has no identity to compare.
// lifted is recorded when a read verified a signature for this same id; the
// gate does not trust the bit. It verifies unfreeze.txt and unfreeze.sig again
// on every read.
//
// PROD api and PROD workers share one state file. combine keeps whichever
// side has seen a freeze the other has already left behind, so a worker whose
// file is behind learns the later freeze and does not adopt an earlier object.
// STG has its own file. It catches up only by reading the object: a freeze it
// has never recorded becomes the latest; an id it has already left behind does
// not. A process with an empty memory treats the freeze the object proves as
// the latest, because it has not seen a later one. Neither file is cleared
// except by the break-glass, which replaces the whole document with
// {"frozen_at":""} on both platforms and sets the object back to never-frozen.
type seenSnap struct {
	id           string
	at           time.Time
	unidentified bool
	seenAt       time.Time
	lifted       bool
	earlier      map[string]struct{}
	earlierUnid  map[string]struct{}
}

func (s seenSnap) remembersFreeze() bool {
	return s.id != "" || s.unidentified || !s.at.IsZero() || len(s.earlier) > 0 || len(s.earlierUnid) > 0
}

func (s seenSnap) clone() seenSnap {
	out := s
	out.earlier = map[string]struct{}{}
	for id := range s.earlier {
		out.earlier[id] = struct{}{}
	}
	out.earlierUnid = map[string]struct{}{}
	for k := range s.earlierUnid {
		out.earlierUnid[k] = struct{}{}
	}
	return out
}

func unidKey(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// newFreezeID returns a fresh lowercase hex identity. Two calls do not share
// a value; nothing in the existing state is an input.
func newFreezeID() (string, error) {
	buf := make([]byte, freezeIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("freeze id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// wellFormedFreezeID reports whether s is a lowercase hex identity of at
// least 128 bits. A counter, a clock, or a prefix of another id is not one.
func wellFormedFreezeID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < freezeIDBytes*2 || len(s)%2 != 0 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// signedFreezeID returns the freeze_id token in an unfreeze text. The value
// is the whole token, so an id that merely prefixes another does not match.
func signedFreezeID(text string) (string, bool) {
	rest, ok := strings.CutPrefix(text, "unfreeze ")
	if !ok {
		return "", false
	}
	field, _, _ := strings.Cut(rest, " ")
	k, v, ok := strings.Cut(field, "=")
	if !ok || k != "freeze_id" || v == "" {
		return "", false
	}
	return v, true
}

// unfreezeNames reports whether text is the sentence for this id and time.
// The id is compared as a whole token, not as a prefix of a longer one.
func unfreezeNames(text, id, frozenAt string) bool {
	got, ok := signedFreezeID(text)
	if !ok || got != strings.TrimSpace(id) {
		return false
	}
	return strings.HasPrefix(text, UnfreezePrefix(id, frozenAt))
}

// advance records one observation. A frozen object this process has not seen
// becomes the latest. An id already left behind does not become the latest
// again. A lifted object is not a new freeze: it marks the remembered freeze
// when the id matches, and it teaches an empty memory what the object proves.
// It does not replace a freeze this process already remembers.
func advance(cur, obs seenSnap) seenSnap {
	out := cur.clone()
	if obs.lifted {
		if !cur.unidentified && cur.id == obs.id {
			out.at = obs.at
			out.lifted = true
			return out
		}
		if !cur.remembersFreeze() {
			out.id = obs.id
			out.at = obs.at
			out.unidentified = false
			out.seenAt = time.Time{}
			out.lifted = true
			return out
		}
		return out
	}
	if obs.unidentified {
		key := unidKey(obs.at)
		if cur.unidentified && unidKey(cur.at) == key {
			return out
		}
		// An object with no id cannot be matched to an earlier one. Seeing it
		// in place is a new freeze, including when frozen_at repeats: ignoring
		// it would leave the previous freeze liftable.
		if cur.id != "" {
			out.earlier[cur.id] = struct{}{}
		}
		if cur.unidentified {
			out.earlierUnid[unidKey(cur.at)] = struct{}{}
		}
		out.id = ""
		out.at = obs.at
		out.unidentified = true
		out.seenAt = obs.seenAt
		out.lifted = false
		delete(out.earlierUnid, key)
		return out
	}
	if obs.id == "" {
		return out
	}
	if !cur.unidentified && cur.id == obs.id {
		out.at = obs.at
		out.lifted = false
		return out
	}
	if _, seen := cur.earlier[obs.id]; seen {
		return out
	}
	if cur.id != "" {
		out.earlier[cur.id] = struct{}{}
	}
	if cur.unidentified {
		out.earlierUnid[unidKey(cur.at)] = struct{}{}
	}
	out.id = obs.id
	out.at = obs.at
	out.unidentified = false
	out.seenAt = time.Time{}
	out.lifted = obs.lifted
	return out
}

func sameIdentity(a, b seenSnap) bool {
	if a.unidentified || b.unidentified {
		return a.unidentified && b.unidentified && unidKey(a.at) == unidKey(b.at)
	}
	return a.id != "" && a.id == b.id
}

func sameSnap(a, b seenSnap) bool {
	if a.id != b.id || a.unidentified != b.unidentified || a.lifted != b.lifted || !a.at.Equal(b.at) || !a.seenAt.Equal(b.seenAt) {
		return false
	}
	if len(a.earlier) != len(b.earlier) || len(a.earlierUnid) != len(b.earlierUnid) {
		return false
	}
	for id := range a.earlier {
		if _, ok := b.earlier[id]; !ok {
			return false
		}
	}
	for k := range a.earlierUnid {
		if _, ok := b.earlierUnid[k]; !ok {
			return false
		}
	}
	return true
}

// combine merges two memories of the same platform. A freeze either side has
// already left behind does not become the latest. When both sides name a
// live freeze the other has not left behind, b is the memory just read from
// the object, so it wins and a is recorded as earlier.
func combine(a, b seenSnap) seenSnap {
	if !a.remembersFreeze() {
		return b.clone()
	}
	if !b.remembersFreeze() {
		return a.clone()
	}
	earlier := map[string]struct{}{}
	for id := range a.earlier {
		earlier[id] = struct{}{}
	}
	for id := range b.earlier {
		earlier[id] = struct{}{}
	}
	earlierUnid := map[string]struct{}{}
	for k := range a.earlierUnid {
		earlierUnid[k] = struct{}{}
	}
	for k := range b.earlierUnid {
		earlierUnid[k] = struct{}{}
	}
	aDead := superseded(a, earlier, earlierUnid)
	bDead := superseded(b, earlier, earlierUnid)
	var win seenSnap
	switch {
	case aDead && bDead:
		// b is the memory just taken from the object. A freeze with no id can
		// be seen again after it was left behind; that observation stands.
		win = b.clone()
	case aDead && !bDead:
		win = b.clone()
	case !aDead && bDead:
		win = a.clone()
	case sameIdentity(a, b):
		win = b.clone()
		if a.unidentified && !a.seenAt.IsZero() && (win.seenAt.IsZero() || a.seenAt.Before(win.seenAt)) {
			win.seenAt = a.seenAt
		}
	default:
		win = b.clone()
		if a.id != "" && !a.unidentified {
			earlier[a.id] = struct{}{}
		}
		if a.unidentified {
			earlierUnid[unidKey(a.at)] = struct{}{}
		}
	}
	if win.id != "" {
		delete(earlier, win.id)
	}
	if win.unidentified {
		delete(earlierUnid, unidKey(win.at))
	}
	win.earlier = earlier
	win.earlierUnid = earlierUnid
	return win
}

func superseded(s seenSnap, earlier, earlierUnid map[string]struct{}) bool {
	if s.unidentified {
		_, ok := earlierUnid[unidKey(s.at)]
		return ok
	}
	if s.id != "" {
		_, ok := earlier[s.id]
		return ok
	}
	return false
}

type seenState struct {
	FrozenAt     string   `json:"frozen_at"`
	FreezeID     string   `json:"freeze_id,omitempty"`
	Unidentified bool     `json:"unidentified,omitempty"`
	SeenAt       string   `json:"seen_at,omitempty"`
	Lifted       bool     `json:"lifted,omitempty"`
	Earlier      []string `json:"earlier,omitempty"`
	EarlierUnid  []string `json:"earlier_unidentified,omitempty"`
}

func decodeSeen(raw []byte) (seenSnap, error) {
	snap := seenSnap{earlier: map[string]struct{}{}, earlierUnid: map[string]struct{}{}}
	if len(bytesTrimSpace(raw)) == 0 {
		return snap, nil
	}
	var st seenState
	if err := json.Unmarshal(raw, &st); err != nil {
		return seenSnap{}, fmt.Errorf("freeze history: %w", err)
	}
	for _, id := range st.Earlier {
		if wellFormedFreezeID(id) {
			snap.earlier[id] = struct{}{}
		}
	}
	for _, k := range st.EarlierUnid {
		snap.earlierUnid[k] = struct{}{}
	}
	if st.FrozenAt != "" {
		at, err := time.Parse(time.RFC3339, st.FrozenAt)
		if err != nil {
			return seenSnap{}, fmt.Errorf("freeze history: %w", err)
		}
		snap.at = at
	}
	if st.SeenAt != "" {
		at, err := time.Parse(time.RFC3339, st.SeenAt)
		if err != nil {
			return seenSnap{}, fmt.Errorf("freeze history: %w", err)
		}
		snap.seenAt = at
	}
	switch {
	case wellFormedFreezeID(st.FreezeID):
		snap.id = st.FreezeID
		snap.lifted = st.Lifted
	case st.Unidentified || st.FreezeID != "" || st.FrozenAt != "":
		// A counter left in an old file, or a freeze with no id, is not an
		// identity a signature can name. Remembering it keeps the gate shut.
		snap.unidentified = true
		snap.id = ""
		snap.lifted = false
	}
	if snap.id != "" {
		delete(snap.earlier, snap.id)
	}
	if snap.unidentified {
		delete(snap.earlierUnid, unidKey(snap.at))
	}
	if !snap.remembersFreeze() {
		snap.lifted = false
	}
	return snap, nil
}

func bytesTrimSpace(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func encodeSeen(s seenSnap) []byte {
	st := seenState{Lifted: s.lifted}
	if !s.at.IsZero() {
		st.FrozenAt = s.at.UTC().Format(time.RFC3339)
	}
	if s.unidentified {
		st.Unidentified = true
		if !s.seenAt.IsZero() {
			st.SeenAt = s.seenAt.UTC().Format(time.RFC3339)
		}
	} else if s.id != "" {
		st.FreezeID = s.id
	}
	if len(s.earlier) > 0 {
		st.Earlier = make([]string, 0, len(s.earlier))
		for id := range s.earlier {
			st.Earlier = append(st.Earlier, id)
		}
		sort.Strings(st.Earlier)
	}
	if len(s.earlierUnid) > 0 {
		st.EarlierUnid = make([]string, 0, len(s.earlierUnid))
		for k := range s.earlierUnid {
			st.EarlierUnid = append(st.EarlierUnid, k)
		}
		sort.Strings(st.EarlierUnid)
	}
	raw, _ := json.Marshal(st)
	return raw
}

func (e *Engine) loadSeenLocked() (seenSnap, error) {
	if e.d.StatePath == "" {
		return e.seen.clone(), nil
	}
	raw, err := statefile.ReadFile(e.d.StatePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return seenSnap{earlier: map[string]struct{}{}, earlierUnid: map[string]struct{}{}}, nil
		}
		return seenSnap{}, err
	}
	return decodeSeen(raw)
}

func (e *Engine) storeSeenLocked(next seenSnap) error {
	if e.d.StatePath == "" {
		e.seen = combine(e.seen, next)
		return nil
	}
	var merged seenSnap
	err := statefile.Update(e.d.StatePath, func(old []byte) ([]byte, error) {
		cur, err := decodeSeen(old)
		if err != nil {
			return nil, err
		}
		merged = combine(cur, next)
		return encodeSeen(merged), nil
	})
	if err != nil {
		return err
	}
	e.seen = merged
	return nil
}

// rememberLocked folds one observation into the memory. The caller holds e.mu.
func (e *Engine) rememberLocked(obs seenSnap) error {
	seen, err := e.loadSeenLocked()
	if err != nil {
		return err
	}
	e.seen = seen
	next := advance(e.seen, obs)
	if sameSnap(next, e.seen) {
		return nil
	}
	return e.storeSeenLocked(next)
}

// blocksLaterLocked reports whether id is not the latest freeze already
// remembered. An empty memory does not block: the object is ahead of this
// process, and the caller records the freeze it just read. The caller holds e.mu.
func (e *Engine) blocksLaterLocked(id string) (bool, error) {
	seen, err := e.loadSeenLocked()
	if err != nil {
		return false, err
	}
	e.seen = seen
	if seen.unidentified {
		return true, nil
	}
	if _, ok := seen.earlier[id]; ok {
		return true, nil
	}
	if seen.id != "" && seen.id != id {
		return true, nil
	}
	if seen.id == "" && seen.remembersFreeze() {
		return true, nil
	}
	return false, nil
}
