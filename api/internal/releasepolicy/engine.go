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
	fz := e.freeze(ctx)
	st.Frozen, st.FreezeReason = fz.frozen, fz.reason
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

// Decision is the result for one action call.
type Decision struct {
	Auto     bool
	PolicyID string
	Pipeline string
	Who      string
	SHAs     map[string]string
	Reasons  []string
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
	if !pol.Allows(d.Pipeline) {
		d.Reasons = []string{d.Pipeline + " is not in the policy's allow list"}
		return d
	}

	var reasons []string
	if pol.Enforced("no_pending_before_db_steps") && contains(pol.DBStepPipelines, d.Pipeline) {
		reasons = append(reasons, d.Pipeline+" has DB steps that only release.sh tracks; start it with release.sh")
	}
	if pol.Enforced("window_held_by_requester") {
		if e.d.Window == nil {
			reasons = append(reasons, "release window: no reader")
		} else if msg := e.d.Window.HeldBy(ctx, d.Pipeline, d.Who); msg != "" {
			reasons = append(reasons, "release window: "+msg)
		}
	}
	reasons = append(reasons, e.diffReasons(ctx, pol, &d, revision)...)
	d.Reasons = reasons
	d.Auto = len(reasons) == 0
	return d
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
		reasons = append(reasons, e.repoReasons(ctx, pol, repo, old, next)...)
	}
	if checkRev && !revFound {
		reasons = append(reasons, "revision "+revision+" is not main, a tag, or a commit on main")
	}
	return reasons
}

func (e *Engine) repoReasons(ctx context.Context, pol *Policy, repo, old, next string) []string {
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
		if hits := RuleHits(pol.Paths, rule, repo, files); len(hits) > 0 {
			shown := hits
			more := ""
			if len(hits) > 5 {
				shown, more = hits[:5], fmt.Sprintf(" (+%d more)", len(hits)-5)
			}
			reasons = append(reasons, fmt.Sprintf("%s %s..%s hits %s: %s%s", repo, short(old), short(next), rule, strings.Join(shown, ", "), more))
		}
	}
	return reasons
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
