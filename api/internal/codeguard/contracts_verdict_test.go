package codeguard

import (
	"fmt"
	"sort"
	"strings"
	"testing"

)

// The contract check (LAW-10: domains speak only through their
// contracts). A domain's Go contract is the list of its exported names other
// domains may use, registered in config/domain-ownership.json under
// domains[].contracts.go_surface, each with the packages that use it. Every
// use of a domain's exported name by another domain's production code (as
// the CI line's ruler counts it, domainscore.Surface.UseList) is either
// registered there, with its user named, or held in
// config/contract-baseline.json, which only shrinks.
//
// Origin: 2026-11-17. The decoupling line measured about 1,100 names other
// domains used and none registered (duty/from-biz-2026-11-16-1925-domain-
// contracts-thin.md); the Owner's law of 11-16 says a domain's contract is
// what others may use and nothing else.

// crossUse is one use the ruler counts: Key is "api/internal/x.Name" (a
// method: "api/internal/x.Type.Method"), Domain the domain that declares
// it, User the package of another domain that names it. It mirrors
// domainscore.Use, which the live check converts.
type crossUse struct{ Key, Domain, User string }

// goSurfaceItem is one registered name of a domain's Go contract.
type goSurfaceItem struct {
	Name  string   `json:"name"`
	Users []string `json:"users"`
	Why   string   `json:"why"`
}

type contractTable struct {
	Domains []struct {
		ID        string `json:"id"`
		Contracts struct {
			GoSurface []goSurfaceItem `json:"go_surface"`
		} `json:"contracts"`
	} `json:"domains"`
}

type baselineFile struct {
	Uses []struct {
		Name string `json:"name"`
		User string `json:"user"`
	} `json:"uses"`
}

// contractVerdict compares the uses with the registered contracts and the
// baseline. It returns what is wrong, one line each, sorted.
func contractVerdict(uses []crossUse, table contractTable, base baselineFile) []string {
	type pair struct{ name, user string }
	registered := map[string]map[string]bool{} // name -> users
	declaredBy := map[string]string{}          // name -> domain that registers it
	var out []string
	for _, d := range table.Domains {
		for _, item := range d.Contracts.GoSurface {
			if prev, dup := declaredBy[item.Name]; dup {
				out = append(out, fmt.Sprintf("%s is registered twice (%s, %s)", item.Name, prev, d.ID))
			}
			declaredBy[item.Name] = d.ID
			if len(item.Users) == 0 {
				out = append(out, fmt.Sprintf("%s (contract of %s) names no user: a contract item says who uses it", item.Name, d.ID))
			}
			if strings.TrimSpace(item.Why) == "" {
				out = append(out, fmt.Sprintf("%s (contract of %s) says no why", item.Name, d.ID))
			}
			registered[item.Name] = map[string]bool{}
			for _, u := range item.Users {
				registered[item.Name][u] = true
			}
		}
	}
	held := map[pair]bool{}
	for _, b := range base.Uses {
		held[pair{b.Name, b.User}] = true
	}
	seen := map[pair]bool{}
	for _, u := range uses {
		p := pair{u.Key, u.User}
		seen[p] = true
		if users, ok := registered[u.Key]; ok {
			if dom := declaredBy[u.Key]; dom != u.Domain {
				out = append(out, fmt.Sprintf("%s is registered by %s but declared in %s", u.Key, dom, u.Domain))
			}
			if !users[u.User] {
				out = append(out, fmt.Sprintf("%s uses %s: add %s to that contract item's users", u.User, u.Key, u.User))
			}
			if held[p] {
				out = append(out, fmt.Sprintf("%s by %s is registered: delete it from config/contract-baseline.json", u.Key, u.User))
			}
			continue
		}
		if !held[p] {
			out = append(out, fmt.Sprintf("%s uses %s, which %s has not registered as its contract: register it (domains[].contracts.go_surface) or use what the contract offers", u.User, u.Key, u.Domain))
		}
	}
	for p := range held {
		if !seen[p] {
			out = append(out, fmt.Sprintf("%s by %s no longer occurs: delete it from config/contract-baseline.json", p.name, p.user))
		}
	}
	for name, users := range registered {
		for user := range users {
			if !seen[pair{name, user}] {
				out = append(out, fmt.Sprintf("%s is registered with user %s, who does not use it: remove the user", name, user))
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestContractVerdictCatches is the positive control of the check above:
// each kind of problem is reported, and a clean case is not.
//
// Origin: 2026-11-17, the control of TestCrossDomainUsesAreContracts.
func TestContractVerdictCatches(t *testing.T) {
	t.Parallel()
	mk := func(name, users, why string) contractTable {
		var tb contractTable
		tb.Domains = append(tb.Domains, struct {
			ID        string `json:"id"`
			Contracts struct {
				GoSurface []goSurfaceItem `json:"go_surface"`
			} `json:"contracts"`
		}{ID: "plan"})
		if name != "" {
			var us []string
			if users != "" {
				us = strings.Split(users, ",")
			}
			tb.Domains[0].Contracts.GoSurface = []goSurfaceItem{{Name: name, Users: us, Why: why}}
		}
		return tb
	}
	base := func(pairs ...string) baselineFile {
		var b baselineFile
		for _, p := range pairs {
			n, u, _ := strings.Cut(p, "@")
			b.Uses = append(b.Uses, struct {
				Name string `json:"name"`
				User string `json:"user"`
			}{n, u})
		}
		return b
	}
	use := func(name, user string) crossUse { return crossUse{Key: name, Domain: "plan", User: user} }
	cases := []struct {
		name  string
		uses  []crossUse
		table contractTable
		base  baselineFile
		want  string // "" means clean
	}{
		{"registered and named", []crossUse{use("p.A", "x")}, mk("p.A", "x", "w"), base(), ""},
		{"held in baseline", []crossUse{use("p.A", "x")}, mk("", "", ""), base("p.A@x"), ""},
		{"neither", []crossUse{use("p.A", "x")}, mk("", "", ""), base(), "has not registered"},
		{"new user", []crossUse{use("p.A", "y")}, mk("p.A", "x", "w"), base(), "add y"},
		{"stale baseline", nil, mk("", "", ""), base("p.A@x"), "no longer occurs"},
		{"registered and still in baseline", []crossUse{use("p.A", "x")}, mk("p.A", "x", "w"), base("p.A@x"), "delete it from"},
		{"no why", []crossUse{use("p.A", "x")}, mk("p.A", "x", ""), base(), "says no why"},
		{"user who no longer uses it", nil, mk("p.A", "x", "w"), base(), "does not use it"},
	}
	for _, c := range cases {
		got := contractVerdict(c.uses, c.table, c.base)
		if c.want == "" && len(got) != 0 {
			t.Errorf("%s: want clean, got %v", c.name, got)
		}
		if c.want != "" && (len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), c.want)) {
			t.Errorf("%s: want a line containing %q, got %v", c.name, c.want, got)
		}
	}
}
