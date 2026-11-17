package headrelease

import "strings"

// Wakes says which bus events are worth a pass for the enabled rows of a
// table (2026-11-17 00:11, PROD: head-release still sent ~400 pipelineruns
// LISTs in 10 minutes after it listed one pipeline only, because every
// ci.run.finished of any pipeline and every change of the release window,
// renewals included, started a pass). A pass is worth it when main moved in
// one of the rows' repositories, a run of a row's pipeline or of a row's own
// CI finished, or the window was let go.
type Wakes struct {
	repos    map[string]bool
	prefixes []string
}

// WakesOf reads the enabled rows of t.
func WakesOf(t Table) Wakes {
	w := Wakes{repos: map[string]bool{}}
	for _, d := range t.Deliverables {
		if !d.Enabled {
			continue
		}
		w.prefixes = append(w.prefixes, d.Pipeline+"-")
		for _, c := range d.Commits {
			w.repos[c.Repo] = true
			if c.TestedWith == "" {
				// the repository's own CI: ci-<name without bifrost->-<id>
				w.prefixes = append(w.prefixes, "ci-"+strings.TrimPrefix(c.Repo, "bifrost-")+"-")
			}
		}
	}
	return w
}

// Worth says whether an event of type typ with subject and change starts a pass.
func (w Wakes) Worth(typ, subject, change string) bool {
	switch typ {
	case "git.main.advanced": // subject <repo>@<sha>
		repo, _, _ := strings.Cut(subject, "@")
		return w.repos[repo]
	case "ci.run.finished": // subject <run>@<sha>
		run, _, _ := strings.Cut(subject, "@")
		for _, p := range w.prefixes {
			if strings.HasPrefix(run, p) {
				return true
			}
		}
		return false
	case "release.window.changed":
		return change == "released" || change == "expired"
	}
	return false
}
