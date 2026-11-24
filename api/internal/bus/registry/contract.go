package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// ContractFields are the parts of a channel a subscriber relies on (RULES.md,
// the contract; Owner 2026-11-16 OWNER_LAW #95): changing one without raising
// version breaks someone silently. publisher, reconcile, consumers, exports
// and replaces are the publisher's own business.
func (c Channel) ContractFields() map[string]string {
	return map[string]string{
		"address":   c.Address,
		"lane":      c.Lane,
		"guarantee": c.Guarantee,
		"key":       c.Key,
		"subject":   c.Subject,
		"ref":       c.Ref,
		"retention": c.Retention,
		"on_unread": c.OnUnread,
	}
}

// ContractHash is a stable fingerprint of a channel's contract fields.
func (c Channel) ContractHash() string {
	f := c.ContractFields()
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([][2]string, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, [2]string{k, f[k]})
	}
	b, _ := json.Marshal(ordered)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// LockEntry is one channel's line in contract.lock.json.
type LockEntry struct {
	Version int    `json:"version"`
	Hash    string `json:"hash"`
}
