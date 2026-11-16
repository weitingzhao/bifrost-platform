package hook

import (
	"fmt"
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Type is the event gate-git writes: main of a repository moved.
const Type = "git.main.advanced"

// Change says how gate-git learned of it: Gitea's push hook, or its own
// comparison of main's head with the ledger.
const (
	ChangePushed     = "pushed"
	ChangeReconciled = "reconciled"
)

var (
	sha40  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
)

// Ref is where a reader of the event reads the repository's state.
func Ref(repo string) string { return "/api/v1/delivery/context?repo=" + repo }

// Body is the event's body: the bus's fixed fields (subject, ref, key,
// change; RULES.md §7) and the facts they are made of.
func Body(repo, commit, before, change string) map[string]any {
	return map[string]any{
		"repo":    repo,
		"commit":  commit,
		"before":  before,
		"subject": repo + "@" + commit,
		"ref":     Ref(repo),
		"key":     repo,
		"change":  change,
	}
}

// The vocabulary of gate-git, declared to the event log from here: the
// publisher brings its type's body with it (bus/registry/rocket.yaml
// registers the type; migration 0025 lets the database take it).
func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			Type: {
				{Name: "repo", Kind: eventlog.Str},
				{Name: "commit", Kind: eventlog.ID},
				{Name: "before", Kind: eventlog.Any},
				{Name: "subject", Kind: eventlog.Str},
				{Name: "ref", Kind: eventlog.Str},
				{Name: "key", Kind: eventlog.Str},
				{Name: "change", Kind: eventlog.Str},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			Type: {
				"repo":   eventlog.Shaped("a repository name", repoRE),
				"commit": eventlog.Shaped("a 40-character lowercase commit", sha40),
				"change": eventlog.OneOf(ChangePushed, ChangeReconciled),
			},
		},
		WholeValues: map[string]map[string]func(v any) string{
			Type: {"before": func(v any) string {
				if s, ok := v.(string); ok && (s == "" || sha40.MatchString(s)) {
					return ""
				}
				return fmt.Sprintf("is %v, not empty or a 40-character lowercase commit", v)
			}},
		},
		After: map[string]func(body map[string]any) error{
			Type: func(body map[string]any) error {
				repo, _ := body["repo"].(string)
				commit, _ := body["commit"].(string)
				if body["subject"] != repo+"@"+commit || body["ref"] != Ref(repo) || body["key"] != repo {
					return eventlog.Invalid("%s: subject, ref and key are made of repo and commit", Type)
				}
				return nil
			},
		},
	})
}
