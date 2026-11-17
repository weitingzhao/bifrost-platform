package registry

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

const lockPath = "contract.lock.json"

// 2026-11-16 (Owner, OWNER_LAW #28: a contract does not change silently):
// contract.lock.json holds, per tenant and type, the version and the
// fingerprint of the fields subscribers rely on. A changed fingerprint with
// the same version, a removed type, or a lowered version is red. Raising
// the version is the deliberate path: regenerate the lock with
// UPDATE_CONTRACT_LOCK=1 go test ./internal/bus/registry/ in the same
// change, and tell the users (the register's consumers). A new type passes
// and is added at the next regeneration.
func TestContractDoesNotChangeSilently(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	now := map[string]map[string]LockEntry{}
	for name, ten := range all {
		now[name] = map[string]LockEntry{}
		for typ, ch := range ten.Channels {
			now[name][typ] = LockEntry{Version: ch.Version, Hash: ch.ContractHash()}
		}
	}
	if os.Getenv("UPDATE_CONTRACT_LOCK") == "1" {
		b, _ := json.MarshalIndent(now, "", "  ")
		if err := os.WriteFile(lockPath, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("%s: %v (generate it with UPDATE_CONTRACT_LOCK=1)", lockPath, err)
	}
	var lock map[string]map[string]LockEntry
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	tenants := make([]string, 0, len(lock))
	for n := range lock {
		tenants = append(tenants, n)
	}
	sort.Strings(tenants)
	for _, name := range tenants {
		for typ, was := range lock[name] {
			is, ok := now[name][typ]
			switch {
			case !ok:
				t.Errorf("%s/%s was removed: removing a topic changes the contract; keep it with a deprecation date first", name, typ)
			case is.Version < was.Version:
				t.Errorf("%s/%s: version went down %d -> %d", name, typ, was.Version, is.Version)
			case is.Hash != was.Hash && is.Version == was.Version:
				t.Errorf("%s/%s: a contract field changed (address, lane, guarantee, key, subject, ref, retention, on_unread) but version stayed %d; raise it and regenerate the lock", name, typ, is.Version)
			case is.Version > was.Version && is.Hash == was.Hash:
				t.Errorf("%s/%s: version raised to %d with no contract change; regenerate the lock", name, typ, is.Version)
			case is.Version > was.Version:
				t.Errorf("%s/%s: version raised to %d; regenerate the lock in this change (UPDATE_CONTRACT_LOCK=1) and tell its consumers", name, typ, is.Version)
			}
		}
	}
}

// The check bites: a channel whose subject changed has another fingerprint.
func TestContractHashSeesAContractChange(t *testing.T) {
	a := Channel{Address: "ev.rocket.{env}.x.y.z", Subject: "<repo>@<sha>", Key: "repo", Version: 1}
	b := a
	b.Subject = "<repo>"
	c := a
	c.Consumers = []string{"someone-new"}
	if a.ContractHash() == b.ContractHash() {
		t.Error("a subject change kept the fingerprint")
	}
	if a.ContractHash() != c.ContractHash() {
		t.Error("a new consumer changed the fingerprint; consumers are not the contract")
	}
}
