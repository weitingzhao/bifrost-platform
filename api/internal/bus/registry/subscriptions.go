package registry

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed subscriptions/*.yaml
var subFiles embed.FS

// Delivery kinds a subscription row may name.
const (
	DeliverWake   = "wake"
	DeliverAction = "action"
	DeliverNotify = "notify"
)

// ByDispatcher marks a row the generic dispatcher delivers; any other value
// names hand-written code that delivers it until it is switched.
const ByDispatcher = "dispatcher"

// Subscription is one row: which events, who is told, how.
type Subscription struct {
	ID        string            `yaml:"id"`
	On        []string          `yaml:"on"`
	Match     string            `yaml:"match"`
	Change    string            `yaml:"change"`
	Deliver   string            `yaml:"deliver"`
	To        string            `yaml:"to"`
	Action    string            `yaml:"action"`
	Params    map[string]string `yaml:"params"`
	Guarantee string            `yaml:"guarantee"`
	By        string            `yaml:"by"`
	// Envs, when set, are the only environments the row is dispatched in
	// (a drill on STG must not wake anyone from PROD).
	Envs []string `yaml:"envs"`
	// Repos are where commit.session looks for the event's commit when the
	// event's ref names no repository (ci.run.finished of a landing run).
	Repos []string `yaml:"repos"`
}

// InEnv says whether the row is dispatched in env.
func (r Subscription) InEnv(env string) bool {
	if len(r.Envs) == 0 {
		return true
	}
	for _, e := range r.Envs {
		if e == env {
			return true
		}
	}
	return false
}

// Client is one declared user of the bus: a reader of the gate (its name is
// the receipt consumer, gate:<route>:<client>) or a durable consumer. A
// client that has not come for longer than LostAfter is shown as lost; a
// name that comes without being declared is shown as new (2026-11-16, the
// Owner: the bus must recognise users coming and going).
type Client struct {
	Name      string `yaml:"name"`
	Kind      string `yaml:"kind"`
	LostAfter string `yaml:"lost_after"`
	Note      string `yaml:"note"`
	// Envs, when set, are the environments it is expected in (head-release
	// runs only in PROD's workers; on STG it is not lost, it is not there).
	Envs []string `yaml:"envs"`
}

// InEnv says whether the client is expected in env.
func (c Client) InEnv(env string) bool {
	if len(c.Envs) == 0 {
		return true
	}
	for _, e := range c.Envs {
		if e == env {
			return true
		}
	}
	return false
}

// Subscriptions is one tenant's table.
type Subscriptions struct {
	Tenant string `yaml:"tenant"`
	// UndeliverableTo is where a wake goes when its recipient is not a
	// registered thread in this environment (2026-11-16, the tenant drill:
	// such a wake was dropped without a word). Empty: logged only.
	UndeliverableTo string         `yaml:"undeliverable_to"`
	Rows            []Subscription `yaml:"subscriptions"`
	Clients         []Client       `yaml:"clients"`
}

var (
	subIDRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	addressRE = regexp.MustCompile(`^(seat|thread|agent):[A-Za-z0-9._:-]{1,128}$`)
	wakeFrom  = map[string]bool{"subject": true, "commit.session": true, "ref.requester": true}
)

// LoadSubscriptions reads every tenant's table compiled into the binary.
func LoadSubscriptions() (map[string]Subscriptions, error) {
	entries, err := subFiles.ReadDir("subscriptions")
	if err != nil {
		return nil, err
	}
	out := map[string]Subscriptions{}
	for _, e := range entries {
		data, err := subFiles.ReadFile("subscriptions/" + e.Name())
		if err != nil {
			return nil, err
		}
		var s Subscriptions
		if err := yaml.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("subscriptions %s: %w", e.Name(), err)
		}
		if want := strings.TrimSuffix(e.Name(), ".yaml"); s.Tenant != want {
			return nil, fmt.Errorf("subscriptions %s: tenant %q is not the file's name", e.Name(), s.Tenant)
		}
		out[s.Tenant] = s
	}
	return out, nil
}

// ActionTier answers an action's tier ("B", "C", ...) and whether the
// catalog has it; the caller passes the actions catalog (registry does not
// import it).
type ActionTier func(id string) (tier string, ok bool)

// CheckSubscriptions checks every table against the registers: a row decides
// who is told and never grants anything. Refused at registration (every
// problem is returned):
//   - an event a tenant may not hear: neither in its own register nor
//     exported to it by another tenant;
//   - an action not in the catalog, or one whose tier needs an approval but
//     the row would run it (an action row only asks: mode request);
//   - a wake with no address it can resolve, or a row with no delivery.
func CheckSubscriptions(tenants map[string]Tenant, subs map[string]Subscriptions, tier ActionTier) error {
	var errs []string
	bad := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	names := make([]string, 0, len(subs))
	for n := range subs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		table := subs[name]
		own, ok := tenants[name]
		if !ok {
			bad("%s: subscriptions for a tenant with no register", name)
			continue
		}
		if table.UndeliverableTo != "" && !addressRE.MatchString(table.UndeliverableTo) {
			bad("%s: undeliverable_to %q is not an address", name, table.UndeliverableTo)
		}
		names := map[string]bool{}
		for _, c := range table.Clients {
			at := name + "/clients/" + c.Name
			if c.Name == "" || names[c.Name] {
				bad("%s: a client needs a name used once", at)
			}
			names[c.Name] = true
			if c.Kind != "reader" && c.Kind != "durable" {
				bad("%s: kind is reader or durable", at)
			}
			if d, err := time.ParseDuration(c.LostAfter); err != nil || d <= 0 {
				bad("%s: lost_after is a duration such as 2h", at)
			}
		}
		seen := map[string]bool{}
		for _, r := range table.Rows {
			at := name + "/" + r.ID
			if !subIDRE.MatchString(r.ID) {
				bad("%s: id is 1 to 63 lowercase letters, digits and dashes", at)
			}
			if seen[r.ID] {
				bad("%s: id used twice", at)
			}
			seen[r.ID] = true
			if len(r.On) == 0 {
				bad("%s: on names no event", at)
			}
			for _, typ := range r.On {
				if !mayHear(tenants, name, own, typ) {
					bad("%s: %s is neither in %s's register nor exported to it", at, typ, name)
				}
			}
			if r.Guarantee != BestEffort && r.Guarantee != Durable && r.Guarantee != "hint" {
				bad("%s: guarantee is hint, best-effort or durable", at)
			}
			if r.By == "" {
				bad("%s: by names who delivers it (dispatcher, or the hand-written code that still does)", at)
			}
			if d := r.Params[ParamDetail]; d != "" {
				if _, _, ok := TaskResult(d); !ok {
					bad("%s: params.detail %q is task-result:<pipeline task>/<result>", at, d)
				}
				for _, typ := range r.On {
					if typ != "ci.run.finished" {
						bad("%s: params.detail reads a run's task result; %s names no run", at, typ)
					}
				}
			}
			switch r.Deliver {
			case DeliverWake:
				if !wakeFrom[r.To] && !addressRE.MatchString(r.To) {
					bad("%s: wake to %q is neither an address nor one of subject, commit.session, ref.requester", at, r.To)
				}
			case DeliverAction:
				t, ok := tier(r.Action)
				switch {
				case !ok:
					bad("%s: action %q is not in the catalog", at, r.Action)
				case t != "B" && r.Params["mode"] != "request":
					bad("%s: action %s is tier %s, which needs an approval; a row may only ask for it (params.mode: request)", at, r.Action, t)
				}
			case DeliverNotify:
			default:
				bad("%s: deliver is wake, action or notify", at)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("bus subscriptions: %s", strings.Join(errs, "\n"))
	}
	return nil
}

func mayHear(tenants map[string]Tenant, name string, own Tenant, typ string) bool {
	if _, ok := own.Channels[typ]; ok {
		return true
	}
	for other, t := range tenants {
		if other == name {
			continue
		}
		if ch, ok := t.Channels[typ]; ok {
			for _, to := range ch.Exports {
				if to == name {
					return true
				}
			}
		}
	}
	return false
}

// ParamDetail is the row parameter naming what a wake adds to its message
// (subscribe.Detail): task-result:<pipeline task>/<result>, the result a
// task of the event's run wrote (a refused landing's reason: land/refusal).
const ParamDetail = "detail"

var taskResultRE = regexp.MustCompile(`^task-result:([a-z0-9]([-a-z0-9]*[a-z0-9])?)/([a-z0-9]([-a-z0-9]*[a-z0-9])?)$`)

// TaskResult reads a params.detail of the form task-result:<task>/<result>.
func TaskResult(detail string) (task, result string, ok bool) {
	m := taskResultRE.FindStringSubmatch(detail)
	if m == nil {
		return "", "", false
	}
	return m[1], m[3], true
}
