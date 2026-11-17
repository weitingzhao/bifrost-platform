package registry

import (
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Agree says whether the body a publisher declared for typ carries what the
// rocket register promises its subscribers: subject, ref, change, and the key
// field when the entry names one. Each publisher's tests call it for its own
// types, so a gate can be rewritten freely as long as this stays green
// (2026-11-16: the approval cards' decided body missed its key "card"; the
// shared check only saw the types its own test binary imported).
func Agree(typ string) error {
	all, err := Load()
	if err != nil {
		return err
	}
	ch, ok := all["rocket"].Channels[typ]
	if !ok {
		return fmt.Errorf("%s is not registered", typ)
	}
	fields, ok := eventlog.BodyOf(typ)
	if !ok {
		return fmt.Errorf("%s has no declared body", typ)
	}
	has := map[string]bool{}
	for _, f := range fields {
		has[f.Name] = !f.Optional
	}
	need := []string{"subject", "ref", "change"}
	if ch.Key != "" {
		need = append(need, "key", ch.Key)
	}
	for _, n := range need {
		if !has[n] {
			return fmt.Errorf("%s: the body does not require %q (register: key %q)", typ, n, ch.Key)
		}
	}
	return nil
}
