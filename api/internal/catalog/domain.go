package catalog

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// One domain's whole page of the map (2026-11-20, law 50: the domains, what
// each owns and what it opens are in one small module): what it owns in the
// table's repository and in the cluster, its contract as written, and its
// lights. Everything comes from what the scan kept; nothing is listed at
// request time.

// Domain is the answer of GET /catalog/domain?id=….
type Domain struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	// Owns: Go packages, console groups and other paths of the table's
	// repository whose row names the domain; Shared marks a row that names
	// more than one domain (law 50 wants one).
	Packages  []Row `json:"packages"`
	Groups    []Row `json:"groups"`
	Paths     []Row `json:"paths"`
	Workloads []Row `json:"workloads"`
	// Contract is the domain's contracts block in the table, as written.
	Contract json.RawMessage `json:"contract"`
	Status   *DomainStatus   `json:"status,omitempty"`
	Note     string          `json:"note,omitempty"`
}

// Row is one row that names the domain.
type Row struct {
	Path   string   `json:"path"`
	Shared []string `json:"shared,omitempty"`
}

func rowsOf(id string, m map[string][]string) []Row {
	out := []Row{}
	for p, ds := range m {
		named, others := false, []string{}
		for _, d := range ds {
			if d == id {
				named = true
			} else {
				others = append(others, d)
			}
		}
		if !named {
			continue
		}
		r := Row{Path: p}
		if len(others) > 0 {
			sort.Strings(others)
			r.Shared = others
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// DomainOf assembles one domain from the kept table, owners and status.
func (s *Scanner) DomainOf(id string) (Domain, bool) {
	out := Domain{Version: Version, ID: id, Packages: []Row{}, Groups: []Row{}, Paths: []Row{}, Workloads: []Row{}}
	k, ok := s.keptSections()
	if !ok {
		out.Note = "no scanned table names its domains yet"
		return out, true
	}
	var domains []json.RawMessage
	_ = json.Unmarshal(k.Domains, &domains)
	found := false
	for _, raw := range domains {
		var d struct {
			ID        string          `json:"id"`
			Contracts json.RawMessage `json:"contracts"`
		}
		if json.Unmarshal(raw, &d) == nil && d.ID == id {
			found, out.Contract = true, d.Contracts
		}
	}
	if !found {
		return out, false
	}
	out.Repo, out.Commit = k.Repo, k.Commit
	if o, _ := s.lastOwners(k.Repo); o != nil {
		out.Packages, out.Groups, out.Paths = rowsOf(id, o.Packages), rowsOf(id, o.Groups), rowsOf(id, o.Paths)
	}
	if o, _ := s.lastOwners(ClusterRepo); o != nil {
		out.Workloads = rowsOf(id, o.Paths)
	}
	if raw, err := s.kept().Read(keptStatus); err == nil && raw != nil {
		var st Status
		if json.Unmarshal(raw, &st) == nil {
			for _, d := range st.Domains {
				if d.ID == id {
					d := d
					out.Status = &d
				}
			}
		}
	}
	if out.Status == nil {
		out.Note = "its lights are not computed yet"
	}
	return out, true
}

// DomainRoute is GET /catalog/domain?id=….
type DomainRoute struct{ Scanner *Scanner }

func (rt DomainRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || len(id) > 64 || !repoName.MatchString(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is a domain id, as GET /catalog/status lists them"})
		return
	}
	d, ok := rt.Scanner.DomainOf(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no domain " + id + " in the scanned table"})
		return
	}
	writeJSON(w, http.StatusOK, d)
}
