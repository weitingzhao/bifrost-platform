// Package releasepolicy lets a signed Owner policy stand in for a manual
// approval of a tier C release.
//
// The policy lives in ConfigMap cicd/bifrost-release-policy (policy.yaml +
// policy.sig), signed with `ssh-keygen -Y sign -n bifrost-release-policy` by
// bifrost-trade-infra/scripts/release/release.sh policy sign. A freeze in
// cicd/bifrost-release-freeze overrides it; an unreadable freeze counts as
// frozen. Tier D is never auto-approved. When anything fails the caller keeps
// the manual approval path.
//
// Additive DDL is an allow-list of statements, not a textual line scan.
// *.py, db_init*, YAML jobs and migrations/** always wait for the Owner.
package releasepolicy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigMap names in the pipelines namespace (cicd).
const (
	PolicyConfigMap = "bifrost-release-policy"
	FreezeConfigMap = "bifrost-release-freeze"
)

// Commands the Owner runs; reminders and refusals print them.
const (
	SignCommand     = "bifrost-trade-infra/scripts/release/release.sh policy sign"
	UnfreezeCommand = "bifrost-trade-infra/scripts/release/release.sh unfreeze"
)

// PathRules are the diff conditions, named as in the policy's conditions map.
var PathRules = []string{"no_ddl", "no_d10_paths", "no_trust_anchor_change"}

const clockSkew = 5 * time.Minute

// PathRule is one entry of paths.json rules: globs that apply to a repo or "*".
type PathRule struct {
	Repo  string   `yaml:"repo"`
	Paths []string `yaml:"paths"`
}

// Policy is policy.yaml as release.sh policy sign writes it (canonical JSON).
type Policy struct {
	Version    int                   `yaml:"version"`
	PolicyID   string                `yaml:"policy_id"`
	SignedAt   string                `yaml:"signed_at"`
	ExpiresAt  string                `yaml:"expires_at"`
	Allow      []string              `yaml:"allow"`
	Conditions map[string]any        `yaml:"conditions"`
	Reminders  Reminders             `yaml:"reminders"`
	CIRepos    []string              `yaml:"ci_repos"`
	DBSteps    DBSteps               `yaml:"db_steps"`
	Pinned     Pinned                `yaml:"pinned"`
	Paths      map[string][]PathRule `yaml:"paths"`
}

// Pinned covers pipelines that ship the commits named in their params rather
// than main: bifrost-deliver-prod ships the commits of an STG run. Keyed by
// pipeline. From is the pipeline whose newest release record must have shipped
// exactly those commits; that record is what makes them "main" for the
// revision rule. Params maps a param to its repo; "revision" is the top-level
// revision, every other key is read from params.params.
type Pinned map[string]PinnedPipeline

type PinnedPipeline struct {
	From   string            `yaml:"from"`
	Params map[string]string `yaml:"params"`
}

// Reminders is the policy's reminder schedule.
type Reminders struct {
	BeforeExpiryHours    []int `yaml:"before_expiry_hours"`
	OnBlockedAfterExpiry *bool `yaml:"on_blocked_after_expiry"`
}

// DBSteps says where the one-off DB steps of a release are committed and
// which pipelines deliver which env. A step is pending for an env when its
// front matter lists the env in envs, has when: before, and does not list the
// env in done. Only the committed state on main counts.
type DBSteps struct {
	Repo      string            `yaml:"repo"`
	Dir       string            `yaml:"dir"`
	Pipelines map[string]string `yaml:"pipelines"`
}

// AdditiveDDL reports whether a no_ddl hit may pass when every hit is an
// allow-listed .sql change (ADR §5, 2026-10-08 revision). It is off unless
// the policy says true. *.py, db_init*, YAML jobs and migrations/** wait
// for the Owner.
func (p *Policy) AdditiveDDL() bool {
	v, ok := p.Conditions["additive_ddl"].(bool)
	return ok && v
}

// Enforced reports whether a condition applies. Only an explicit false turns
// one off; a missing condition is enforced.
func (p *Policy) Enforced(name string) bool {
	if v, ok := p.Conditions[name].(bool); ok && !v {
		return false
	}
	return true
}

// Allows reports whether name is in the allow list.
func (p *Policy) Allows(name string) bool {
	for _, a := range p.Allow {
		if a == name {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// DefaultReminderHours is 14, 3 and 1 days before expiry (ADR §5: 90-day policies).
var DefaultReminderHours = []int{14 * 24, 3 * 24, 24}

// ReminderHours is the reminder schedule, largest first.
func (p *Policy) ReminderHours() []int {
	hours := append([]int(nil), p.Reminders.BeforeExpiryHours...)
	if len(hours) == 0 {
		hours = append(hours, DefaultReminderHours...)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(hours)))
	return hours
}

// WindowLabel names a reminder window: whole days as "14d", otherwise "36h".
func WindowLabel(hours int) string {
	if hours > 0 && hours%24 == 0 {
		return fmt.Sprintf("%dd", hours/24)
	}
	return fmt.Sprintf("%dh", hours)
}

type policyEval struct {
	policy    *Policy
	valid     bool
	reasons   []string
	expiresAt time.Time
	signedAt  time.Time
}

// evaluatePolicy verifies the signature against anchor and the validity window.
func evaluatePolicy(data map[string]string, found bool, readErr error, anchor string, now time.Time) policyEval {
	ev := policyEval{}
	text := ""
	if data != nil {
		text = data["policy.yaml"]
	}
	switch {
	case readErr != nil:
		ev.reasons = append(ev.reasons, "cannot read ConfigMap cicd/"+PolicyConfigMap+": "+readErr.Error())
		return ev
	case !found || strings.TrimSpace(text) == "":
		ev.reasons = append(ev.reasons, "no signed policy (ConfigMap cicd/"+PolicyConfigMap+" is missing or empty)")
		return ev
	case strings.TrimSpace(data["policy.sig"]) == "":
		ev.reasons = append(ev.reasons, "policy.sig is missing")
		return ev
	}
	if err := verifyAnchored([]byte(data["policy.sig"]), []byte(text), NamespacePolicy, anchor); err != nil {
		ev.reasons = append(ev.reasons, "policy signature: "+err.Error())
		return ev
	}
	var p Policy
	if err := yaml.Unmarshal([]byte(text), &p); err != nil {
		ev.reasons = append(ev.reasons, "policy.yaml does not parse: "+err.Error())
		return ev
	}
	ev.policy = &p
	if p.Version != 1 {
		ev.reasons = append(ev.reasons, fmt.Sprintf("policy version %d is not 1", p.Version))
	}
	signed, err1 := time.Parse(time.RFC3339, p.SignedAt)
	expires, err2 := time.Parse(time.RFC3339, p.ExpiresAt)
	if err1 != nil || err2 != nil {
		ev.reasons = append(ev.reasons, "policy has no signed_at / expires_at")
	} else {
		ev.signedAt, ev.expiresAt = signed, expires
		if signed.After(now.Add(clockSkew)) {
			ev.reasons = append(ev.reasons, "policy signed_at "+p.SignedAt+" is in the future")
		}
		if !expires.After(now) {
			ev.reasons = append(ev.reasons, "policy "+p.PolicyID+" expired at "+p.ExpiresAt)
		}
	}
	ev.valid = len(ev.reasons) == 0
	return ev
}

type freezeEval struct {
	frozen   bool
	reason   string
	frozenAt time.Time
}

// evaluateFreeze mirrors policy_check.freeze_state. seen is the newest
// frozen_at ever observed, so an old signed unfreeze cannot lift a new freeze.
func evaluateFreeze(data map[string]string, found bool, readErr error, anchor string, seen time.Time) freezeEval {
	if readErr != nil {
		return freezeEval{frozen: true, reason: "freeze ConfigMap cicd/" + FreezeConfigMap + " is unreadable: " + readErr.Error()}
	}
	if !found || data == nil {
		return freezeEval{frozen: true, reason: "freeze ConfigMap cicd/" + FreezeConfigMap + " is missing"}
	}
	flag := strings.ToLower(strings.TrimSpace(data["frozen"]))
	at := strings.TrimSpace(data["frozen_at"])
	parsedAt, atErr := time.Parse(time.RFC3339, at)
	if flag == "true" {
		who, reason := data["who"], data["reason"]
		if who == "" {
			who = "unknown"
		}
		if reason == "" {
			reason = "no reason given"
		}
		ev := freezeEval{frozen: true, reason: fmt.Sprintf("frozen by %s at %s: %s", who, orQ(at), reason)}
		if atErr == nil {
			ev.frozenAt = parsedAt
		}
		return ev
	}
	if flag != "false" {
		return freezeEval{frozen: true, reason: fmt.Sprintf("freeze flag is %q, not false", flag)}
	}
	if at == "" {
		if !seen.IsZero() {
			return freezeEval{frozen: true, reason: "a freeze from " + seen.UTC().Format(time.RFC3339) + " was seen and its record is gone"}
		}
		return freezeEval{}
	}
	if atErr != nil {
		return freezeEval{frozen: true, reason: fmt.Sprintf("frozen_at %q is not a timestamp", at)}
	}
	if !seen.IsZero() && parsedAt.Before(seen) {
		return freezeEval{frozen: true, reason: "unfreeze is for " + at + "; a later freeze (" + seen.UTC().Format(time.RFC3339) + ") was seen"}
	}
	text := data["unfreeze.txt"]
	if !strings.HasPrefix(text, "unfreeze frozen_at="+at+" ") {
		return freezeEval{frozen: true, reason: "no signed unfreeze for the freeze at " + at}
	}
	if err := verifyAnchored([]byte(data["unfreeze.sig"]), []byte(text), NamespaceUnfreeze, anchor); err != nil {
		return freezeEval{frozen: true, reason: "unfreeze signature: " + err.Error()}
	}
	return freezeEval{frozenAt: parsedAt}
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

var (
	globMu    sync.Mutex
	globCache = map[string]*regexp.Regexp{}
)

// globRegexp matches policy_check.glob_regex: "**/" is any directory prefix,
// "**" anything, "*" one path segment, "?" one character.
func globRegexp(pattern string) *regexp.Regexp {
	globMu.Lock()
	defer globMu.Unlock()
	if re, ok := globCache[pattern]; ok {
		return re
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); {
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 3
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i += 2
		case pattern[i] == '*':
			b.WriteString("[^/]*")
			i++
		case pattern[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
			i++
		}
	}
	b.WriteString("$")
	re := regexp.MustCompile(b.String())
	globCache[pattern] = re
	return re
}

// PathMatches reports whether a repo-relative path matches a glob.
func PathMatches(path, pattern string) bool {
	return globRegexp(pattern).MatchString(strings.TrimLeft(path, "/"))
}

// RuleHits lists the files in repo that a path rule forbids.
func RuleHits(table map[string][]PathRule, rule, repo string, files []string) []string {
	seen := map[string]bool{}
	for _, entry := range table[rule] {
		if entry.Repo != "*" && entry.Repo != repo {
			continue
		}
		for _, f := range files {
			for _, p := range entry.Paths {
				if PathMatches(f, p) {
					seen[f] = true
					break
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
