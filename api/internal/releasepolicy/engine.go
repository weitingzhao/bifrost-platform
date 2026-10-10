package releasepolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// ConfigMaps reads ConfigMaps in the pipelines namespace. found=false with a
// nil error means NotFound.
type ConfigMaps interface {
	ConfigMap(ctx context.Context, name string) (data map[string]string, found bool, err error)
}

// Git answers questions about repos on the in-cluster Gitea mirror.
type Git interface {
	MainHead(ctx context.Context, repo string) (string, error)
	TagCommit(ctx context.Context, repo, tag string) (sha string, found bool, err error)
	CommitExists(ctx context.Context, repo, sha string) (bool, error)
	ChangedFiles(ctx context.Context, repo, from, to string) ([]string, error)
	// FileAt is a file's text at ref; found is false when the path does not exist there.
	FileAt(ctx context.Context, repo, ref, path string) (text string, found bool, err error)
	// ListDir lists the file names (not subdirectories) in dir at ref.
	ListDir(ctx context.Context, repo, ref, dir string) ([]string, error)
}

// CI reports whether a Succeeded ci-* run exists for repo at sha.
type CI interface {
	Succeeded(ctx context.Context, repo, sha string) (bool, error)
}

// Window returns "" when who holds an open release window covering pipeline,
// otherwise why not.
type Window interface {
	HeldBy(ctx context.Context, pipeline, who string) string
}

// Deployed returns the commits the newest release record of pipeline shipped,
// and the repos that record could not resolve.
type Deployed interface {
	Deployed(ctx context.Context, pipeline string) (repos map[string]string, missing []string, err error)
}

// Deps are the engine's inputs. StatePath is a statefile path for the newest
// freeze seen; empty keeps it in memory only.
type Deps struct {
	ConfigMaps ConfigMaps
	Writer     ConfigMapWriter
	Git        Git
	CI         CI
	Window     Window
	Deployed   Deployed
	StatePath  string
	Anchor     string
	Now        func() time.Time
}

// Engine decides whether a tier C action may run on the signed policy.
type Engine struct {
	d       Deps
	mu      sync.Mutex
	seenMem time.Time
}

// New returns an engine. A nil Now uses the wall clock.
func New(d Deps) *Engine {
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Engine{d: d}
}

// Status is GET /api/v1/release-policy and what reminders read.
type Status struct {
	Valid            bool     `json:"valid"`
	PolicyID         string   `json:"policy_id,omitempty"`
	SignedAt         string   `json:"signed_at,omitempty"`
	ExpiresAt        string   `json:"expires_at,omitempty"`
	RemainingSeconds int64    `json:"remaining_seconds"`
	Expired          bool     `json:"expired"`
	Reasons          []string `json:"reasons"`
	Allow            []string `json:"allow"`
	Frozen           bool     `json:"frozen"`
	FreezeReason     string   `json:"freeze_reason,omitempty"`
	FrozenAt         string   `json:"frozen_at,omitempty"`
	ReminderWindows  []string `json:"reminder_windows"`
	Anchor           string   `json:"anchor_fingerprint"`
	SignCommand      string   `json:"sign_command"`
	UnfreezeCommand  string   `json:"unfreeze_command"`
	CheckedAt        string   `json:"checked_at"`

	policy *Policy
}

// Status reads both ConfigMaps and evaluates them now.
func (e *Engine) Status(ctx context.Context) Status {
	now := e.d.Now()
	st := Status{
		Reasons:         []string{},
		Allow:           []string{},
		Anchor:          e.d.Anchor,
		SignCommand:     SignCommand,
		UnfreezeCommand: UnfreezeCommand,
		CheckedAt:       now.Format(time.RFC3339),
	}
	pdata, pfound, perr := e.read(ctx, PolicyConfigMap)
	ev := evaluatePolicy(pdata, pfound, perr, e.d.Anchor, now)
	st.Valid = ev.valid
	st.Reasons = append(st.Reasons, ev.reasons...)
	if ev.policy != nil {
		st.policy = ev.policy
		st.PolicyID = ev.policy.PolicyID
		st.Allow = append(st.Allow, ev.policy.Allow...)
		if !ev.expiresAt.IsZero() {
			st.SignedAt = ev.signedAt.UTC().Format(time.RFC3339)
			st.ExpiresAt = ev.expiresAt.UTC().Format(time.RFC3339)
			st.RemainingSeconds = int64(ev.expiresAt.Sub(now) / time.Second)
			st.Expired = !ev.expiresAt.After(now)
		}
	}
	hours := DefaultReminderHours
	if ev.policy != nil {
		hours = ev.policy.ReminderHours()
	}
	for _, h := range hours {
		st.ReminderWindows = append(st.ReminderWindows, WindowLabel(h))
	}
	fz := e.freeze(ctx)
	st.Frozen, st.FreezeReason = fz.frozen, fz.reason
	if fz.frozen && !fz.frozenAt.IsZero() {
		st.FrozenAt = fz.frozenAt.UTC().Format(time.RFC3339)
	}
	if st.Valid {
		setExpiresIn(st.RemainingSeconds)
	} else {
		setExpiresIn(0)
	}
	return st
}

func (e *Engine) read(ctx context.Context, name string) (map[string]string, bool, error) {
	if e.d.ConfigMaps == nil {
		return nil, false, errors.New("no cluster client")
	}
	return e.d.ConfigMaps.ConfigMap(ctx, name)
}

type seenState struct {
	FrozenAt string `json:"frozen_at"`
}

// freeze evaluates the freeze ConfigMap and records a newer frozen_at.
func (e *Engine) freeze(ctx context.Context) freezeEval {
	data, found, err := e.read(ctx, FreezeConfigMap)
	e.mu.Lock()
	defer e.mu.Unlock()
	seen, seenErr := e.loadSeenLocked()
	if seenErr != nil {
		return freezeEval{frozen: true, reason: "freeze history unreadable: " + seenErr.Error()}
	}
	if err == nil && found && strings.EqualFold(strings.TrimSpace(data["frozen"]), "true") {
		if at, perr := time.Parse(time.RFC3339, strings.TrimSpace(data["frozen_at"])); perr == nil && at.After(seen) {
			if werr := e.storeSeenLocked(at); werr != nil {
				slog.Warn("release policy: cannot record freeze", "err", werr)
			}
			seen = at
		}
	}
	return evaluateFreeze(data, found, err, e.d.Anchor, seen)
}

func (e *Engine) loadSeenLocked() (time.Time, error) {
	if e.d.StatePath == "" {
		return e.seenMem, nil
	}
	raw, err := statefile.ReadFile(e.d.StatePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	var st seenState
	if err := json.Unmarshal(raw, &st); err != nil {
		return time.Time{}, fmt.Errorf("freeze history: %w", err)
	}
	if st.FrozenAt == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, st.FrozenAt)
}

func (e *Engine) storeSeenLocked(at time.Time) error {
	e.seenMem = at
	if e.d.StatePath == "" {
		return nil
	}
	raw, _ := json.Marshal(seenState{FrozenAt: at.UTC().Format(time.RFC3339)})
	return statefile.WriteFile(e.d.StatePath, raw, 0o644)
}

// Decision is the result for one action call. Clauses are the policy terms
// that were checked and held, in the order checked; the audit records them.
type Decision struct {
	Auto        bool
	PolicyID    string
	Pipeline    string
	Who         string
	SHAs        map[string]string
	Reasons     []string
	Clauses     []string
	AdditiveDDL []string
}

// ClauseText is the audit's clauses= value.
func (d Decision) ClauseText() string {
	if len(d.Clauses) == 0 {
		return "-"
	}
	return strings.Join(d.Clauses, ",")
}

// FreezeSet reports a freeze that someone actually set: the ConfigMap is
// readable, present and evaluates as frozen. Tier B releases, which never
// needed an approval, stop only for this; a missing or unreadable freeze
// ConfigMap only withholds auto-approval.
func (e *Engine) FreezeSet(ctx context.Context) (bool, string) {
	_, found, err := e.read(ctx, FreezeConfigMap)
	if err != nil || !found {
		return false, ""
	}
	fz := e.freeze(ctx)
	return fz.frozen, fz.reason
}

// SHAText is the audit's sha= value: repo=commit pairs in name order.
func (d Decision) SHAText() string {
	names := make([]string, 0, len(d.SHAs))
	for n := range d.SHAs {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+"="+d.SHAs[n])
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

const (
	releaseAction = "start_pipeline_run"
	// Gitea caps a compare listing; at this size the list may be cut short,
	// and a missing file could be the one a rule forbids.
	compareFileCap = 100
)

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Decide reports whether this call may run without a manual approval. Any
// failure to establish a fact is a reason, so the answer defaults to no.
func (e *Engine) Decide(ctx context.Context, actionID string, tier actions.Tier, params map[string]any) Decision {
	d := Decision{SHAs: map[string]string{}}
	switch tier {
	case actions.TierD:
		d.Reasons = []string{"tier D is never auto-approved"}
		return d
	case actions.TierC:
	default:
		d.Reasons = []string{"only tier C actions can be auto-approved"}
		return d
	}
	return e.Evaluate(ctx, actionID, params)
}

// Evaluate runs the policy's checks on a release without the tier rule.
// Auto means the policy covers it. The guard uses it directly for tier B
// releases, which run either way, so the audit shows whether the policy
// would have covered them.
func (e *Engine) Evaluate(ctx context.Context, actionID string, params map[string]any) Decision {
	d := Decision{SHAs: map[string]string{}}
	if actionID != releaseAction {
		d.Reasons = []string{actionID + " is not a release; the policy covers " + releaseAction + " only"}
		return d
	}
	d.Pipeline = str(params["name"])
	d.Who = str(params["who"])
	revision := str(params["revision"])
	if revision == "" {
		revision = "main"
	}

	st := e.Status(ctx)
	if st.Frozen {
		d.Reasons = []string{"releases are frozen: " + st.FreezeReason + " (lift: " + UnfreezeCommand + ")"}
		return d
	}
	if !st.Valid || st.policy == nil {
		d.Reasons = append(append([]string{}, st.Reasons...), "sign a policy: "+SignCommand)
		return d
	}
	pol := st.policy
	d.PolicyID = pol.PolicyID
	d.Clauses = append(d.Clauses, "signature", "unexpired", "not_frozen")
	if !pol.Allows(d.Pipeline) {
		d.Reasons = []string{d.Pipeline + " is not in the policy's allow list"}
		return d
	}
	d.Clauses = append(d.Clauses, "allow:"+d.Pipeline)

	var reasons []string
	if _, delivers := pol.DBSteps.Pipelines[d.Pipeline]; delivers && pol.Enforced("no_pending_before_db_steps") {
		if r := e.dbStepReasons(ctx, pol, d.Pipeline); len(r) > 0 {
			reasons = append(reasons, r...)
		} else {
			d.Clauses = append(d.Clauses, "no_pending_before_db_steps")
		}
	}
	if pol.Enforced("window_held_by_requester") {
		if e.d.Window == nil {
			reasons = append(reasons, "release window: no reader")
		} else if msg := e.d.Window.HeldBy(ctx, d.Pipeline, d.Who); msg != "" {
			reasons = append(reasons, "release window: "+msg)
		} else {
			d.Clauses = append(d.Clauses, "window_held_by_requester")
		}
	}
	var diff []string
	pin, pinnedPipeline := pol.Pinned[d.Pipeline]
	if pinnedPipeline {
		diff = e.pinnedReasons(ctx, pol, &d, pin, params)
	} else {
		diff = e.diffReasons(ctx, pol, &d, revision)
	}
	reasons = append(reasons, diff...)
	if len(diff) == 0 {
		switch {
		case pinnedPipeline:
			d.Clauses = append(d.Clauses, "pinned_to:"+pin.From)
		case pol.Enforced("revision"):
			d.Clauses = append(d.Clauses, "revision")
		}
		if pol.Enforced("ci_succeeded") {
			d.Clauses = append(d.Clauses, "ci_succeeded")
		}
		for _, rule := range PathRules {
			if !pol.Enforced(rule) {
				continue
			}
			if rule == "no_ddl" && len(d.AdditiveDDL) > 0 {
				d.Clauses = append(d.Clauses, "additive_ddl")
				continue
			}
			d.Clauses = append(d.Clauses, rule)
		}
	}
	d.Reasons = reasons
	d.Auto = len(reasons) == 0
	return d
}

// dbStepReasons lists the before-deliver DB steps still pending for the env
// the pipeline delivers, from the files committed on main.
func (e *Engine) dbStepReasons(ctx context.Context, pol *Policy, pipeline string) []string {
	env, ok := pol.DBSteps.Pipelines[pipeline]
	if !ok {
		return nil
	}
	if pol.DBSteps.Repo == "" || pol.DBSteps.Dir == "" {
		return []string{pipeline + " delivers " + env + ", but the policy names no db_steps repo and dir"}
	}
	if e.d.Git == nil {
		return []string{"no reader for the DB steps on Gitea"}
	}
	names, err := e.d.Git.ListDir(ctx, pol.DBSteps.Repo, "main", pol.DBSteps.Dir)
	if err != nil {
		return []string{"cannot list " + pol.DBSteps.Repo + "/" + pol.DBSteps.Dir + ": " + err.Error()}
	}
	var pending []string
	for _, name := range names {
		if !strings.HasSuffix(name, ".md") || strings.EqualFold(name, "README.md") {
			continue
		}
		text, found, err := e.d.Git.FileAt(ctx, pol.DBSteps.Repo, "main", pol.DBSteps.Dir+"/"+name)
		if err != nil || !found {
			return []string{"cannot read DB step " + name + ": " + errText(err)}
		}
		step, ok := parseDBStep(text)
		if !ok {
			return []string{"DB step " + name + " has no readable front matter"}
		}
		if step.pendingBefore(env) {
			pending = append(pending, step.id)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	sort.Strings(pending)
	return []string{fmt.Sprintf("%d DB step(s) due before the %s deliver are not marked done on main: %s",
		len(pending), env, strings.Join(pending, ", "))}
}

// diffReasons resolves the commit each repo will ship, then checks the
// revision, CI and path conditions against what the newest record shipped.
func (e *Engine) diffReasons(ctx context.Context, pol *Policy, d *Decision, revision string) []string {
	if e.d.Deployed == nil || e.d.Git == nil {
		return []string{"no reader for deployed commits or Gitea"}
	}
	deployed, missing, err := e.d.Deployed.Deployed(ctx, d.Pipeline)
	if err != nil {
		return []string{"deployed commits for " + d.Pipeline + ": " + err.Error()}
	}
	if len(deployed) == 0 {
		return []string{"no release record for " + d.Pipeline + ", so the deployed commits are unknown"}
	}
	var reasons []string
	for _, repo := range missing {
		reasons = append(reasons, repo+": the newest release record has no commit for it")
	}
	repos := make([]string, 0, len(deployed))
	for r := range deployed {
		repos = append(repos, r)
	}
	sort.Strings(repos)

	checkRev := pol.Enforced("revision")
	revFound := revision == "main"
	for _, repo := range repos {
		old := deployed[repo]
		head, err := e.d.Git.MainHead(ctx, repo)
		if err != nil || head == "" {
			reasons = append(reasons, repo+": cannot read main: "+errText(err))
			continue
		}
		next := head
		switch {
		case revision == "main":
		case fullSHA.MatchString(revision):
			ok, err := e.d.Git.CommitExists(ctx, repo, revision)
			if err != nil {
				reasons = append(reasons, repo+": cannot look up "+short(revision)+": "+err.Error())
				continue
			}
			if ok {
				next, revFound = revision, true
				if checkRev && revision != head {
					reasons = append(reasons, "revision "+short(revision)+" is not the head of main in "+repo)
				}
			}
		default:
			sha, ok, err := e.d.Git.TagCommit(ctx, repo, revision)
			if err != nil {
				reasons = append(reasons, repo+": cannot look up tag "+revision+": "+err.Error())
				continue
			}
			if ok {
				next, revFound = sha, true
			}
		}
		d.SHAs[repo] = next
		if old == "" {
			reasons = append(reasons, repo+": the newest release record has no commit for it")
			continue
		}
		if old == next {
			continue
		}
		reasons = append(reasons, e.repoReasons(ctx, pol, d, repo, old, next)...)
	}
	if checkRev && !revFound {
		reasons = append(reasons, "revision "+revision+" is not main, a tag, or a commit on main")
	}
	return reasons
}

// pinnedReasons checks a pipeline that ships the commits in its params: each
// must be what the newest record of the source pipeline shipped, and the CI
// and path rules run from the newest record of this pipeline to those commits.
func (e *Engine) pinnedReasons(ctx context.Context, pol *Policy, d *Decision, pin PinnedPipeline, params map[string]any) []string {
	if e.d.Deployed == nil || e.d.Git == nil {
		return []string{"no reader for deployed commits or Gitea"}
	}
	from := pin.From
	if from == "" {
		return []string{"the policy pins " + d.Pipeline + " to no source pipeline"}
	}
	pinned := map[string]string{}
	inner, _ := params["params"].(map[string]any)
	for key, repo := range pin.Params {
		sha := str(inner[key])
		if key == "revision" {
			sha = str(params["revision"])
		}
		if sha != "" {
			pinned[repo] = sha
		}
	}
	if len(pinned) == 0 {
		return []string{d.Pipeline + " ships pinned commits, and the request names none"}
	}
	source, _, err := e.d.Deployed.Deployed(ctx, from)
	if err != nil {
		return []string{"deployed commits for " + from + ": " + err.Error()}
	}
	deployed, missing, err := e.d.Deployed.Deployed(ctx, d.Pipeline)
	if err != nil {
		return []string{"deployed commits for " + d.Pipeline + ": " + err.Error()}
	}
	if len(deployed) == 0 {
		return []string{"no release record for " + d.Pipeline + ", so the deployed commits are unknown"}
	}
	var reasons []string
	for _, repo := range missing {
		reasons = append(reasons, repo+": the newest release record has no commit for it")
	}
	repos := make([]string, 0, len(pinned))
	for r := range pinned {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	for _, repo := range repos {
		next := pinned[repo]
		d.SHAs[repo] = next
		if !fullSHA.MatchString(next) {
			reasons = append(reasons, repo+": pinned revision "+next+" is not a full commit id")
			continue
		}
		if source[repo] != next {
			reasons = append(reasons, fmt.Sprintf("%s: pinned %s is not what the newest %s record shipped (%s)", repo, short(next), from, orQ(short(source[repo]))))
			continue
		}
		old := deployed[repo]
		if old == "" {
			reasons = append(reasons, repo+": the newest release record has no commit for it")
			continue
		}
		if old == next {
			continue
		}
		reasons = append(reasons, e.repoReasons(ctx, pol, d, repo, old, next)...)
	}
	unpinned := make([]string, 0)
	for repo := range deployed {
		if _, ok := pinned[repo]; !ok {
			unpinned = append(unpinned, repo)
		}
	}
	sort.Strings(unpinned)
	for _, repo := range unpinned {
		reasons = append(reasons, repo+": the request pins no commit for it")
	}
	return reasons
}

func (e *Engine) repoReasons(ctx context.Context, pol *Policy, d *Decision, repo, old, next string) []string {
	var reasons []string
	if pol.Enforced("ci_succeeded") && contains(pol.CIRepos, repo) {
		if e.d.CI == nil {
			reasons = append(reasons, repo+": no CI reader")
		} else if ok, err := e.d.CI.Succeeded(ctx, repo, next); err != nil {
			reasons = append(reasons, repo+": cannot read CI runs: "+err.Error())
		} else if !ok {
			reasons = append(reasons, repo+" "+short(next)+" has no Succeeded ci run")
		}
	}
	files, err := e.d.Git.ChangedFiles(ctx, repo, old, next)
	switch {
	case err != nil:
		return append(reasons, repo+": cannot read the diff "+short(old)+".."+short(next)+": "+err.Error())
	case len(files) == 0:
		return append(reasons, repo+": the diff "+short(old)+".."+short(next)+" lists no files")
	case len(files) >= compareFileCap:
		return append(reasons, fmt.Sprintf("%s: the diff %s..%s lists %d files, which may be truncated", repo, short(old), short(next), len(files)))
	}
	for _, rule := range PathRules {
		if !pol.Enforced(rule) {
			continue
		}
		hits := RuleHits(pol.Paths, rule, repo, files)
		if len(hits) == 0 {
			continue
		}
		if rule == "no_ddl" && pol.AdditiveDDL() {
			additive, why := e.additiveDDL(ctx, repo, old, next, hits)
			if why == "" {
				d.AdditiveDDL = append(d.AdditiveDDL, additive...)
				continue
			}
			reasons = append(reasons, fmt.Sprintf("%s %s..%s changes DDL that is not additive-only (tier D, ask the Owner): %s",
				repo, short(old), short(next), why))
			continue
		}
		shown := hits
		more := ""
		if len(hits) > 5 {
			shown, more = hits[:5], fmt.Sprintf(" (+%d more)", len(hits)-5)
		}
		reasons = append(reasons, fmt.Sprintf("%s %s..%s hits %s: %s%s", repo, short(old), short(next), rule, strings.Join(shown, ", "), more))
	}
	return reasons
}

// maxDDLFiles bounds the per-file reads one decision makes.
const maxDDLFiles = 20

// additiveDDL classifies each DDL hit. A path that is not a classifiable
// .sql file waits for the Owner. why is empty when every file only adds
// allow-listed statements; the list names those files.
func (e *Engine) additiveDDL(ctx context.Context, repo, old, next string, hits []string) ([]string, string) {
	if len(hits) > maxDDLFiles {
		return nil, fmt.Sprintf("%d DDL files changed, more than the %d this check reads", len(hits), maxDDLFiles)
	}
	out := make([]string, 0, len(hits))
	for _, path := range hits {
		if why := ddlPathReason(path); why != "" {
			return nil, why
		}
		before, _, err := e.d.Git.FileAt(ctx, repo, old, path)
		if err != nil {
			return nil, path + ": cannot read at " + short(old) + ": " + err.Error()
		}
		after, found, err := e.d.Git.FileAt(ctx, repo, next, path)
		if err != nil {
			return nil, path + ": cannot read at " + short(next) + ": " + err.Error()
		}
		if !found {
			return nil, path + " is deleted"
		}
		if ok, why := ClassifyDDL(before, after); !ok {
			return nil, path + ": " + why
		}
		out = append(out, repo+":"+path)
	}
	return out, ""
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func errText(err error) string {
	if err == nil {
		return "empty"
	}
	return err.Error()
}
