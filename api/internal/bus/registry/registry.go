// Package registry is the bus's register of event types: one AsyncAPI 3.0
// file per tenant, each channel an event type with its x-bifrost terms
// (publisher, guarantee, what it is reconciled against, which way a reader
// fails when it cannot read the state). It is the single source of the
// closed vocabulary of the event lane (api/internal/bus/RULES.md §3-§5):
// the relay publishes only registered types, and NATS permissions are
// generated from it. It imports no domain package.
package registry

import (
	"embed"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var files embed.FS

// Guarantees a channel may promise (RULES.md §4).
const (
	BestEffort = "best-effort"
	Durable    = "durable"
	Ordered    = "ordered"
)

// Channel is one event type's register entry.
type Channel struct {
	Type      string `yaml:"-"`
	Address   string `yaml:"address"`
	Version   int    `yaml:"version"`
	Lane      string `yaml:"lane"`
	Guarantee string `yaml:"guarantee"`
	Key       string `yaml:"key"`
	Subject   string `yaml:"subject"`
	Publisher string `yaml:"publisher"`
	// Domain is the domain (config/domain-ownership.json) whose work the
	// event tells of, for the Bus page's map: the publisher is often a gate
	// of the bus itself. Not a contract field.
	Domain    string   `yaml:"domain"`
	Ref       string   `yaml:"ref"`
	Reconcile string   `yaml:"reconcile"`
	OnUnread  string   `yaml:"on_unread"`
	Retention string   `yaml:"retention"`
	Consumers []string `yaml:"consumers"`
	Exports   []string `yaml:"exports"`
	Replaces  []string `yaml:"replaces"`
	// DisplayOnly marks a topic published for watching (the Bus page, a
	// seat's look), which no consumer is meant to act on: the listening
	// reading leaves it out of "heard" and does not call it deaf. Not a
	// contract field.
	DisplayOnly bool `yaml:"display_only"`
}

// Tenant is one tenant's register.
type Tenant struct {
	Name     string
	Channels map[string]Channel
}

type file struct {
	AsyncAPI string `yaml:"asyncapi"`
	Tenant   string `yaml:"x-bifrost-tenant"`
	Channels map[string]struct {
		Address string  `yaml:"address"`
		Terms   Channel `yaml:"x-bifrost"`
	} `yaml:"channels"`
}

var (
	typeRE   = regexp.MustCompile(`^[a-z0-9]+\.[a-z0-9]+\.[a-z0-9]+$`)
	tenantRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	retainRE = regexp.MustCompile(`^[1-9][0-9]*[dh]$`)
)

// Parse reads one tenant file and checks every channel (RULES.md §5). Every
// problem found is returned, not only the first.
func Parse(name string, data []byte) (Tenant, error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Tenant{}, fmt.Errorf("%s: %w", name, err)
	}
	var errs []string
	bad := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	if f.AsyncAPI != "3.0.0" {
		bad("asyncapi is %q, want 3.0.0", f.AsyncAPI)
	}
	if !tenantRE.MatchString(f.Tenant) {
		bad("x-bifrost-tenant %q is not a tenant name", f.Tenant)
	}
	if want := f.Tenant + ".yaml"; path.Base(name) != want {
		bad("file %s holds tenant %q; name it %s", name, f.Tenant, want)
	}
	t := Tenant{Name: f.Tenant, Channels: map[string]Channel{}}
	for typ, ch := range f.Channels {
		c := ch.Terms
		c.Type, c.Address = typ, ch.Address
		where := func(format string, args ...any) { bad("%s: "+format, append([]any{typ}, args...)...) }
		if !typeRE.MatchString(typ) {
			where("an event type is three lower-case segments, domain.object.change")
		}
		if want := "ev." + f.Tenant + ".{env}." + typ; c.Address != want {
			where("address is %q, want %q", c.Address, want)
		}
		if c.Version < 1 {
			where("version must be 1 or more")
		}
		if c.Lane != "event" {
			where("lane is %q: only the event lane carries traffic (the command lane is reserved)", c.Lane)
		}
		switch c.Guarantee {
		case BestEffort, Durable:
		case Ordered:
			if c.Key == "" {
				where("an ordered channel names its key")
			}
		default:
			where("guarantee %q is not best-effort, durable or ordered", c.Guarantee)
		}
		for name, v := range map[string]string{"subject": c.Subject, "publisher": c.Publisher, "ref": c.Ref, "reconcile": c.Reconcile} {
			if strings.TrimSpace(v) == "" {
				where("%s is empty", name)
			}
		}
		if r := strings.TrimSpace(c.Reconcile); r == "none" || (strings.HasPrefix(r, "none") && !strings.HasPrefix(r, "none: ")) {
			where("reconcile none needs its reason: \"none: <why>\"")
		}
		switch c.OnUnread {
		case "closed", "open", "stale":
		default:
			where("on_unread %q is not closed, open or stale", c.OnUnread)
		}
		if !retainRE.MatchString(c.Retention) {
			where("retention %q is not like 30d or 12h", c.Retention)
		}
		if len(c.Consumers) == 0 {
			where("no consumers: a channel nobody may read is a dead road")
		}
		t.Channels[typ] = c
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return t, fmt.Errorf("%s:\n  %s", name, strings.Join(errs, "\n  "))
	}
	return t, nil
}

// Load reads and checks every tenant file compiled into the binary.
func Load() (map[string]Tenant, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	out := map[string]Tenant{}
	var errs []string
	for _, e := range entries {
		data, err := files.ReadFile(e.Name())
		if err != nil {
			return nil, err
		}
		t, err := Parse(e.Name(), data)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		out[t.Name] = t
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("bus registry: %s", strings.Join(errs, "\n"))
	}
	return out, nil
}

// Types is every registered event type of a tenant, sorted.
func (t Tenant) Types() []string {
	out := make([]string, 0, len(t.Channels))
	for typ := range t.Channels {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

// NATSSubject is the NATS subject a channel's events go out on in env.
func (c Channel) NATSSubject(tenant, env string) string {
	return "ev." + tenant + "." + env + "." + c.Type
}
