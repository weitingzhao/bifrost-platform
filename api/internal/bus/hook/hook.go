// Package hook is gate-git, the bus's gate for Git (api/internal/bus/RULES.md
// §1 E3, §7): it turns "main of a repository moved" into a git.main.advanced
// event in the ledger, from Gitea's push hook (POST /api/v1/bus/hooks/gitea)
// and from its own comparison of main's head with the ledger every five
// minutes. The relay takes the event to the bus from there. It imports no
// domain package: the database and the reader of main's head are handed in.
package hook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
)

// Settings. The hook answers 503 and writes nothing without its secret.
const (
	EnvSecret = "BUS_GITEA_HOOK_SECRET"
	EnvRepos  = "BUS_GIT_REPOS"
	// DefaultRepos are the repositories whose push hooks are installed:
	// bifrost-trade-infra since card #597 (2026-11-18).
	DefaultRepos = "bifrost-platform,bifrost-trade-infra"
	// Actor is who writes the events: the gate, a service of the platform.
	Actor = "service:gate-git"
)

// Stream is the ledger stream of a repository's main: one per key, so only
// events of the same repository are chained one after another (RULES.md §7).
func Stream(repo string) string { return "git:" + repo }

// ReposFromEnv is the repositories gate-git watches: BUS_GIT_REPOS, a comma
// list, or DefaultRepos.
func ReposFromEnv() []string {
	v := strings.TrimSpace(os.Getenv(EnvRepos))
	if v == "" {
		v = DefaultRepos
	}
	var out []string
	for _, r := range strings.Split(v, ",") {
		if r = strings.TrimSpace(r); repoRE.MatchString(r) && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// ensureActor registers service:gate-git as an actor once (an event names
// its actor, a registered one): as the platform registers a seat, under the
// registration lock, with an actor_registered event written by the
// controller.
func ensureActor(ctx context.Context, db eventlog.DB) error {
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		return actor.RegisterController(ctx, tx, Actor, "Bus gate-git", "the bus's gate for Git, registered at its first write")
	})
}

// LastCommit is the commit the ledger last said main of repo is at ("" when
// it has said nothing).
func LastCommit(ctx context.Context, db eventlog.DB) func(repo string) (string, error) {
	return func(repo string) (string, error) {
		var commit string
		err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
			return lastCommit(ctx, tx, repo, &commit)
		})
		return commit, err
	}
}

func lastCommit(ctx context.Context, tx pgx.Tx, repo string, commit *string) error {
	last, _, err := eventlog.Last(ctx, tx, Stream(repo))
	if err != nil {
		return err
	}
	*commit, _ = last.Body["commit"].(string)
	return nil
}

// Advance appends git.main.advanced for repo at commit, unless the stream's
// last event already has that commit: then it writes nothing (a push seen
// twice, or seen by the hook and by the reconciliation). The stream's head is
// locked first, so two writers of one repository take turns.
func Advance(ctx context.Context, db eventlog.DB, repo, commit, before, change string) (bool, error) {
	if !repoRE.MatchString(repo) || !sha40.MatchString(commit) {
		return false, eventlog.Invalid("%q@%q is not a repository at a commit", repo, commit)
	}
	if err := ensureActor(ctx, db); err != nil {
		return false, err
	}
	appended := false
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		appended = false
		if err := eventlog.LockStream(ctx, tx, Stream(repo)); err != nil {
			return err
		}
		var last string
		if err := lastCommit(ctx, tx, repo, &last); err != nil {
			return err
		}
		if last == commit {
			return nil
		}
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(repo), Type: Type, Actor: Actor,
			Body: Body(repo, commit, before, change)}); err != nil {
			return err
		}
		appended = true
		return nil
	})
	return appended, err
}

// Hook serves Gitea's push hook. DB is nil while the ledger is not connected.
type Hook struct {
	Secret []byte
	Repos  []string
	DB     func() eventlog.DB
}

// New is the hook of this process's settings.
func New(db func() eventlog.DB) *Hook {
	return &Hook{Secret: []byte(os.Getenv(EnvSecret)), Repos: ReposFromEnv(), DB: db}
}

// maxBody bounds a push payload (Gitea lists the pushed commits in it).
const maxBody = 5 << 20

type push struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Repository struct {
		Name string `json:"name"`
	} `json:"repository"`
}

func answer(w http.ResponseWriter, code int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// ServeHTTP is POST /api/v1/bus/hooks/gitea. It holds no bearer: Gitea signs
// the raw body with the hook's secret (X-Gitea-Signature, HMAC-SHA256, hex),
// and an unsigned or wrongly signed request is refused before it is read
// further. Only a push to refs/heads/main of a watched repository writes.
func (h *Hook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(h.Secret) == 0 {
		answer(w, http.StatusServiceUnavailable, map[string]any{"error": EnvSecret + " is not set: the hook is off"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(raw) > maxBody {
		answer(w, http.StatusBadRequest, map[string]any{"error": "the body could not be read or is over 5 MiB"})
		return
	}
	if !Signed(h.Secret, raw, r.Header.Get("X-Gitea-Signature")) {
		answer(w, http.StatusUnauthorized, map[string]any{"error": "bad signature"})
		return
	}
	if ev := r.Header.Get("X-Gitea-Event"); ev != "" && ev != "push" {
		answer(w, http.StatusAccepted, map[string]any{"ignored": "event " + ev})
		return
	}
	var p push
	if jerr := json.Unmarshal(raw, &p); jerr != nil {
		answer(w, http.StatusBadRequest, map[string]any{"error": "not a push payload: " + jerr.Error()})
		return
	}
	repo := p.Repository.Name
	switch {
	case p.Ref != "refs/heads/main":
		answer(w, http.StatusAccepted, map[string]any{"ignored": "ref " + p.Ref})
		return
	case !slices.Contains(h.Repos, repo):
		answer(w, http.StatusAccepted, map[string]any{"ignored": "repository " + repo + " is not watched"})
		return
	case !sha40.MatchString(p.After) || strings.Trim(p.After, "0") == "":
		answer(w, http.StatusAccepted, map[string]any{"ignored": "no commit main points at"})
		return
	}
	before := p.Before
	if !sha40.MatchString(before) || strings.Trim(before, "0") == "" {
		before = ""
	}
	var db eventlog.DB
	if h.DB != nil {
		db = h.DB()
	}
	if db == nil {
		answer(w, http.StatusServiceUnavailable, map[string]any{"error": "the ledger is not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	appended, err := Advance(ctx, db, repo, p.After, before, ChangePushed)
	if err != nil {
		answer(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	answer(w, http.StatusOK, map[string]any{"repo": repo, "commit": p.After, "appended": appended})
}

// Signed says whether sig is the hex HMAC-SHA256 of body under secret.
func Signed(secret, body []byte, sig string) bool {
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(got) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// Reconciler compares main's head of each watched repository with what the
// ledger last said, and appends git.main.advanced (change reconciled) when
// they differ: a hook Gitea did not send, or one that failed, is made up
// within Every.
type Reconciler struct {
	DB    func() eventlog.DB
	Repos []string
	// Head reads main's head the way head-release does.
	Head  func(ctx context.Context, repo string) (string, error)
	Every time.Duration
	Logf  func(format string, args ...any)
}

func (c *Reconciler) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// Pass compares each repository once.
func (c *Reconciler) Pass(ctx context.Context) error {
	var db eventlog.DB
	if c.DB != nil {
		db = c.DB()
	}
	if db == nil || c.Head == nil {
		return nil
	}
	var errs []error
	for _, repo := range c.Repos {
		head, err := c.Head(ctx, repo)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		last, err := LastCommit(ctx, db)(repo)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if last == head {
			continue
		}
		appended, err := Advance(ctx, db, repo, head, last, ChangeReconciled)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if appended {
			c.logf("bus gate-git: %s main is at %s, the ledger said %q: recorded (reconciled)", repo, head, last)
		}
	}
	return errors.Join(errs...)
}

// Run passes every Every (5 min) until ctx ends.
func (c *Reconciler) Run(ctx context.Context) {
	every := c.Every
	if every <= 0 {
		every = 5 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		pctx, cancel := context.WithTimeout(ctx, time.Minute)
		if err := c.Pass(pctx); err != nil && ctx.Err() == nil {
			c.logf("bus gate-git: reconcile: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// MainReader answers where main of a repository is.
type MainReader = func(ctx context.Context, repo string) (string, error)

// MainOf is the one way display readers ask where main of repo is: the bus
// ledger's last git.main.advanced when gate-git watches repo (the Gitea hook
// records a push within seconds; the workers reconcile every 5 minutes), else
// fallback (Gitea). Adding a repository to BUS_GIT_REPOS moves every reader of
// it onto the ledger; nothing else changes. on says this environment keeps
// git.main.advanced in the ledger; reader names the asker in the readers' count.
//
// Gates (release policy, head-release, migrations, the daily full CI) keep
// asking Gitea: the ledger is a hint for them, the source is the authority
// (nextgen/run-readers.md, the main-head table).
func MainOf(on bool, ledger func() eventlog.DB, reader string, fallback MainReader) MainReader {
	var last MainReader
	if on {
		last = func(ctx context.Context, repo string) (string, error) {
			db := ledger()
			if db == nil {
				return "", nil
			}
			commit, err := LastCommit(ctx, db)(repo)
			if err == nil && gitread.CommitID.MatchString(commit) {
				readerseen.Saw(reader)
			}
			return commit, err
		}
	}
	return ledgeredMain(ReposFromEnv(), last, fallback)
}

// ledgeredMain reads last for watched repositories and fallback for the
// rest, and for a watched one the ledger has said nothing about (or could
// not be read). A nil fallback answers "" (the caller asks Gitea itself).
func ledgeredMain(watched []string, last, fallback MainReader) MainReader {
	return func(ctx context.Context, repo string) (string, error) {
		if last != nil && slices.Contains(watched, repo) {
			if commit, err := last(ctx, repo); err == nil && gitread.CommitID.MatchString(commit) {
				return commit, nil
			}
		}
		if fallback == nil {
			return "", nil
		}
		return fallback(ctx, repo)
	}
}
