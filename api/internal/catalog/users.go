package catalog

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Who uses each package (2026-11-21, Foundation's ask; the map line scans,
// Foundation wrote the measure in biz-align/bus-readings-spec-2026-11-21.md
// section 2). Foundation judged ownership wrong three times that day by
// counting production imports only: a package's tests and the bus's events
// use packages too. So every round keeps, per package, its users by kind:
// prod (the imports of production code), test (the imports of _test.go
// files, internal and external) and event (a topic's publisher and
// subscriber packages). The imports are read as the domain graph reads them
// (unitplan.ReadGoModule, no Go toolchain); the bus registry names
// components, not packages, so event is empty per package until it does and
// the events are counted between domains (Reference kind "event").

// GoPackage is one package of the table's repository as its module reads it:
// imports and test imports by package directory, inside the module only.
type GoPackage struct {
	Dir         string
	Domain      string
	Imports     []string
	TestImports []string
}

// EventUse is one event type going from the domain that publishes it to a
// domain that consumes it (a durable client's domain, a subscription row's
// target domain).
type EventUse struct {
	Type, From, To string
}

// Uses is what the status reads to count uses: the packages at Commit, the
// base domain (the table's default), and the bus's events.
type Uses struct {
	Commit   string
	Base     string
	Packages []GoPackage
	Events   []EventUse
	// EventsMeasured is false when the registry did not read.
	EventsMeasured bool
}

// UseLists is one list per kind of use.
type UseLists struct {
	Prod  []string `json:"prod"`
	Test  []string `json:"test"`
	Event []string `json:"event"`
}

// PackageUsers is one package's users and their domains.
type PackageUsers struct {
	Package     string   `json:"package"`
	Domain      string   `json:"domain"`
	Users       UseLists `json:"users"`
	UserDomains UseLists `json:"user_domains"`
}

// UsersDoc is the answer of GET /catalog/users.
type UsersDoc struct {
	Version  int            `json:"version"`
	At       time.Time      `json:"at"`
	Repo     string         `json:"repo"`
	Commit   string         `json:"commit"`
	Measured bool           `json:"measured"`
	Note     string         `json:"note,omitempty"`
	Packages []PackageUsers `json:"packages"`
}

const keptUsers = "users"

// packageUsers lists every package's users: a package is not its own user.
func packageUsers(pkgs []GoPackage) []PackageUsers {
	domainOf := map[string]string{}
	for _, p := range pkgs {
		domainOf[p.Dir] = p.Domain
	}
	prod, test := map[string][]string{}, map[string][]string{}
	for _, p := range pkgs {
		for _, q := range p.Imports {
			if q != p.Dir {
				prod[q] = append(prod[q], p.Dir)
			}
		}
		for _, q := range p.TestImports {
			if q != p.Dir {
				test[q] = append(test[q], p.Dir)
			}
		}
	}
	out := make([]PackageUsers, 0, len(pkgs))
	for _, p := range pkgs {
		u := PackageUsers{Package: p.Dir, Domain: p.Domain,
			Users:       UseLists{Prod: sorted(prod[p.Dir]), Test: sorted(test[p.Dir]), Event: []string{}},
			UserDomains: UseLists{Prod: domainsOf(prod[p.Dir], domainOf), Test: domainsOf(test[p.Dir], domainOf), Event: []string{}}}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

func domainsOf(users []string, domainOf map[string]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, u := range users {
		if d := domainOf[u]; d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// keepUsers keeps the users of the table's repository, and hands a measured
// answer to UsersChanged (the bus's catalog.users.changed writes only the
// packages whose users differ from their last event).
func (s *Scanner) keepUsers(ctx context.Context, repo string, u Uses, measured bool, err error) {
	doc := UsersDoc{Version: Version, At: s.now(), Repo: repo, Commit: u.Commit, Measured: measured && err == nil, Packages: []PackageUsers{}}
	switch {
	case err != nil:
		doc.Note = "the packages did not read: " + err.Error()
	case !measured:
		doc.Note = "the packages were not measured"
	default:
		doc.Packages = packageUsers(u.Packages)
	}
	raw, mErr := json.Marshal(doc)
	if mErr != nil {
		return
	}
	if wErr := s.kept().Write(keptUsers, raw); wErr != nil {
		slog.Warn("catalog: keep users", "err", wErr)
	}
	if doc.Measured && s.UsersChanged != nil {
		s.UsersChanged(ctx, doc)
	}
}

// onceUses reads in.Uses at most once for one round: the references and the
// kept users read the same answer.
func onceUses(ctx context.Context, f func(context.Context, string) (Uses, bool, error), repo string) func(context.Context, string) (Uses, bool, error) {
	u, measured, err := f(ctx, repo)
	return func(context.Context, string) (Uses, bool, error) { return u, measured, err }
}

// UsersRoute is GET /catalog/users[?package=dir]: the users last kept, of
// every package or of one (404 for a package the kept answer does not have).
type UsersRoute struct{ Scanner *Scanner }

func (rt UsersRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := rt.Scanner.kept().Read(keptUsers)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if raw == nil {
		writeJSON(w, http.StatusOK, UsersDoc{Version: Version, Packages: []PackageUsers{}, Note: "not computed yet"})
		return
	}
	pkg := r.URL.Query().Get("package")
	if pkg == "" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		return
	}
	var doc UsersDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	for _, p := range doc.Packages {
		if p.Package == pkg {
			writeJSON(w, http.StatusOK, p)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "no package " + pkg + " at " + doc.Commit + ": a package is its directory from the repository root, as GET /catalog/users lists them"})
}

// mixedDomain is the domain of a package several domains share, as the
// domain graph names it (domainscore.Mixed): an import through it joins no
// two domains.
const mixedDomain = "mixed"

// RegistryEvents is every event type from the domain the registry files it
// under to each domain that consumes it: a durable client the topic names, a
// subscription row on it (its target domain). The registry is the one this
// binary carries.
func RegistryEvents() ([]EventUse, bool) {
	tenants, err := registry.Load()
	if err != nil {
		return nil, false
	}
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		return nil, false
	}
	var out []EventUse
	seen := map[[3]string]bool{}
	add := func(typ, from, to string) {
		k := [3]string{typ, from, to}
		if from != "" && to != "" && !seen[k] {
			seen[k] = true
			out = append(out, EventUse{Type: typ, From: from, To: to})
		}
	}
	for name, tn := range tenants {
		st := subs[name]
		clients := map[string]string{}
		for _, c := range st.Clients {
			if c.Kind == "durable" {
				clients[c.Name] = c.Domain
			}
		}
		for typ, ch := range tn.Channels {
			for _, c := range ch.Consumers {
				add(typ, ch.Domain, clients[c])
			}
			for _, r := range st.Rows {
				for _, on := range r.On {
					if on == typ {
						add(typ, ch.Domain, r.TargetDomain())
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	return out, true
}
