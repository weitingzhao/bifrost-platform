package registry

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/problemloop"
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

// ByWebhook marks a row the alert webhook delivers itself, in the request
// that wrote the alert, without the bus (2026-11-20, TD-588): an alarm about
// the bus must not ride the bus, or it is lost exactly when it matters. Only
// alert.rule.fired rows may say it.
const ByWebhook = "webhook"

// Subscription is one row: which events, who is told, how.
type Subscription struct {
	ID    string   `yaml:"id"`
	On    []string `yaml:"on"`
	Match string   `yaml:"match"`
	// Except: an event whose subject holds any of these is not this row's,
	// though it matches the rest (2026-11-19, LAW-12: a line's own alerts wake
	// that line, not the duty row that hears every alert). One value or a list.
	Except    Substrings        `yaml:"except"`
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

// TargetDomain is the domain a row's delivery reaches, for the Bus page's
// map: a wake reaches a seat (coordination, the seats' domain), a notify the
// Owner's phone (approvals); an action is run by the platform for whoever asked,
// and names none.
func (r Subscription) TargetDomain() string {
	switch r.Deliver {
	case DeliverWake:
		return "coordination"
	case DeliverNotify:
		return "approvals"
	}
	return ""
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
// name that comes without being declared is shown as new (2026-11-16,
// ISSUE-34: the bus recognises users coming and going).
type Client struct {
	Name      string `yaml:"name"`
	Kind      string `yaml:"kind"`
	LostAfter string `yaml:"lost_after"`
	Note      string `yaml:"note"`
	// Envs, when set, are the environments it is expected in (head-release
	// runs only in PROD's workers; on STG it is not lost, it is not there).
	Envs []string `yaml:"envs"`
	// Domain is the domain whose code runs a durable client (config/
	// domain-ownership.json), for the Bus page's map: an arc runs from a
	// topic's domain to its readers' (2026-11-17).
	Domain string `yaml:"domain"`
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
	wakeFrom  = map[string]bool{"subject": true, "commit.session": true, "ref.requester": true, "requester": true}
)

// LoadSubscriptions reads every tenant's table compiled into the binary.
func LoadSubscriptions() (map[string]Subscriptions, error) {
	subSrc := source(subFiles, "subscriptions")
	entries, err := fs.ReadDir(subSrc, "subscriptions")
	if err != nil {
		return nil, err
	}
	out := map[string]Subscriptions{}
	for _, e := range entries {
		data, err := fs.ReadFile(subSrc, "subscriptions/"+e.Name())
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
			if r.By == ByWebhook && (len(r.On) != 1 || r.On[0] != "alert.rule.fired" || r.Deliver != DeliverWake) {
				bad("%s: by webhook is only for a wake on alert.rule.fired (the alert webhook delivers it)", at)
			}
			if d := r.Params[ParamDetail]; d != "" {
				names, ok := DetailNames(d)
				if !ok {
					bad("%s: params.detail %q is task-result:<pipeline task>/<name>[,<name>…] or %s", at, d, DetailOpenRegressions)
				}
				if req := r.Params[ParamRequire]; req != "" && !slices.Contains(names, req) {
					bad("%s: params.require %q is not one of params.detail's names", at, req)
				}
				for _, typ := range r.On {
					if d != DetailOpenRegressions && typ != "ci.run.finished" {
						bad("%s: params.detail reads a run's task result; %s names no run", at, typ)
					}
				}
			}
			if k, has := r.Params[problemloop.ParamAskVerdict]; has && !problemloop.KnownKind(k) {
				bad("%s: params.%s %q is %s or %s", at, problemloop.ParamAskVerdict, k, problemloop.KindAlert, problemloop.KindDelivery)
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
				if r.To != NotifyTo {
					bad("%s: notify goes to %q (the Owner's phone through the notice relay), not %q", at, NotifyTo, r.To)
				}
				if r.Params[ParamTitle] == "" {
					bad("%s: notify needs params.title", at)
				}
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
// (subscribe.Detail): task-result:<pipeline task>/<name>[,<name>…], each
// name a result the task of the event's run wrote, or else one of its params
// (a refused landing's reason: land/refusal; a tripwire: land/tripwire,
// commit,change-ids,branch).
const ParamDetail = "detail"

// ParamRequire names one of the detail's names: the row wakes only when it
// is not empty (2026-11-17: a landing that touched a watched path, the
// tripwire; every other landing wakes nobody). A detail that cannot be read
// still wakes, saying so.
const ParamRequire = "require"

// ParamTitle and ParamClick are a notify row's push: its title and the link
// it opens, each with {type}, {subject}, {change} and {ref_id} (the last
// segment of the event's ref) filled in (2026-11-17, approval.card.created
// pages the Owner through the bus).
const (
	ParamTitle = "title"
	ParamClick = "click"
)

// NotifyTo is the one target a notify row has: the Owner's phone, through
// the platform's notice relay.
const NotifyTo = "owner"

var (
	taskResultsRE = regexp.MustCompile(`^task-result:([a-z0-9]([-a-z0-9]*[a-z0-9])?)/(.+)$`)
	detailNameRE  = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

// TaskResults reads a params.detail of the form
// task-result:<task>/<name>[,<name>…].
// DetailOpenRegressions is the detail of the known regressions on main still
// open (bus/regressions), one name: open, a line per regression, empty when
// none (2026-11-17: the seat starting a PROD release is woken with them, and
// only when there are any). It reads no run, so any topic may ask for it.
const DetailOpenRegressions = "regressions:open"

// DetailNames is the names a detail answers: a run's task results, or open.
func DetailNames(detail string) ([]string, bool) {
	if detail == DetailOpenRegressions {
		return []string{"open"}, true
	}
	_, names, ok := TaskResults(detail)
	return names, ok
}

func TaskResults(detail string) (task string, names []string, ok bool) {
	m := taskResultsRE.FindStringSubmatch(detail)
	if m == nil {
		return "", nil, false
	}
	for _, n := range strings.Split(m[3], ",") {
		if !detailNameRE.MatchString(n) || slices.Contains(names, n) {
			return "", nil, false
		}
		names = append(names, n)
	}
	return m[1], names, true
}

// Substrings is a row field written as one string or a list of them
// (2026-11-26: the duty row needed a second exception beside ComputePain).
type Substrings []string

// UnmarshalYAML takes a scalar as a one-item list.
func (s *Substrings) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Value == "" {
			*s = nil
		} else {
			*s = Substrings{n.Value}
		}
		return nil
	}
	var list []string
	if err := n.Decode(&list); err != nil {
		return err
	}
	*s = list
	return nil
}

// In reports whether text holds any of them.
func (s Substrings) In(text string) bool {
	for _, sub := range s {
		if sub != "" && strings.Contains(text, sub) {
			return true
		}
	}
	return false
}
