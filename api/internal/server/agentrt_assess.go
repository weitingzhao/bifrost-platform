package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

// What the runtime's assessments read from the platform (TD-391,
// store/assess.go): the cards the runtime asked that are open and the
// approvals waiting for the Owner, as he sees them; and, for each, the
// commits on main that tie to it and the ledger items it names. Every read
// here writes nothing: the question store by RuleCards (questions_wire.go),
// the approvals by Waiting (no sweep), the repositories through the Gitea
// mirror.

// waitingApprovals is the approvals the assessments read.
type waitingApprovals interface {
	Waiting() ([]approvals.Approval, error)
}

// agentRuntimeAssessable lists what is assessed now; nil when the platform
// has neither a question store nor approvals.
func (s *Server) agentRuntimeAssessable() func(context.Context) ([]store.AssessItem, error) {
	var ap waitingApprovals
	if s.approvals != nil {
		ap = s.approvals
	}
	return assessable(s.questionRuntime.agentRuntimeOpenCards(), ap)
}

// assessable lists the open cards (cards, nil for none) and the approvals
// waiting (ap, nil for none); with an error, those it could read.
func assessable(cards func(context.Context) ([]store.AssessItem, error), ap waitingApprovals) func(context.Context) ([]store.AssessItem, error) {
	if cards == nil && ap == nil {
		return nil
	}
	return func(ctx context.Context) ([]store.AssessItem, error) {
		var items []store.AssessItem
		var errs []error
		if cards != nil {
			list, err := cards(ctx)
			if err != nil {
				errs = append(errs, err)
			}
			items = append(items, list...)
		}
		if ap != nil {
			waiting, err := ap.Waiting()
			if err != nil {
				errs = append(errs, fmt.Errorf("the approvals: %w", err))
			}
			for _, a := range waiting {
				if a.Number > 0 {
					items = append(items, approvalItem(a, waiting))
				}
			}
		}
		return items, errors.Join(errs...)
	}
}

// approvalRepoKeys are the key params that name a repository.
var approvalRepoKeys = []string{"repo", "repository", "repos"}

// approvalItem is an approval as its assessment reads it, with the others
// waiting beside it. Its params are read as the Owner's card shows them (the
// key params the action's catalog chose), never whole.
func approvalItem(a approvals.Approval, waiting []approvals.Approval) store.AssessItem {
	title := a.Summary
	if title == "" {
		title = a.Action
	}
	rec := map[string]any{"action": a.Action, "tier": a.Tier, "status": a.Status, "requester": a.Requester,
		"created_at": a.CreatedAt.UTC().Format(time.RFC3339), "expires_at": a.ExpiresAt.UTC().Format(time.RFC3339)}
	for k, v := range map[string]string{"env": a.Env, "summary": a.Summary, "reason": a.Reason, "rollback": a.Rollback,
		"runner": a.Runner, "work_id": a.WorkID} {
		if v != "" {
			rec[k] = v
		}
	}
	var repos []string
	if len(a.KeyParams) > 0 {
		kp := map[string]any{}
		for k, v := range a.KeyParams {
			kp[k] = v
			if slices.Contains(approvalRepoKeys, k) {
				for _, r := range strings.FieldsFunc(v, func(c rune) bool { return c == ',' || c == ' ' }) {
					repos = append(repos, r)
				}
			}
		}
		rec["key_params"] = kp
	}
	it := store.AssessItem{Kind: store.AssessApproval, ID: strconv.Itoa(a.Number), Title: title, Body: a.Reason,
		Options: store.ApprovalOptions(), CreatedAt: a.CreatedAt, Record: rec, Repos: repos}
	for _, o := range waiting {
		if o.Number > 0 && o.Number != a.Number {
			it.Others = append(it.Others, store.AssessOther{Number: o.Number, Action: o.Action, Status: o.Status, Summary: o.Summary,
				CreatedAt: o.CreatedAt})
		}
	}
	return it
}

// Bounds of an assessment's read of the repositories.
const (
	evidenceRepos = 4  // repositories whose main is read
	evidencePages = 10 // pages of 50 commits of each main
	// broadItem: an item that more commits than this name in the window is
	// a programme (W-31), not evidence about one card.
	broadItem = 8
	ledgerTTL = 10 * time.Minute
)

// agentRuntimeEvidence reads an assessment's evidence through the delivery's
// Gitea; nil without the delivery service. The ledger is the file
// AGENTRT_LEDGER names ("<repository>/<path>", read at main); unset, the
// assessments read none and say so.
func (s *Server) agentRuntimeEvidence() store.EvidenceReader {
	if s.delivery == nil {
		return nil
	}
	repo, path := ledgerFromEnv()
	access := s.delivery.GiteaAccess
	return evidenceReader(func(ctx context.Context) (lineage.Access, error) {
		a, err := access(ctx)
		return lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass}, err
	}, &http.Client{Timeout: 20 * time.Second}, repo, path)
}

// ledgerFromEnv is AGENTRT_LEDGER split into its repository and path.
func ledgerFromEnv() (string, string) {
	repo, path, ok := strings.Cut(strings.Trim(strings.TrimSpace(os.Getenv("AGENTRT_LEDGER")), "/"), "/")
	if !ok || repo == "" || path == "" {
		return "", ""
	}
	return repo, path
}

func evidenceReader(access func(context.Context) (lineage.Access, error), client *http.Client, ledgerRepo, ledgerPath string) store.EvidenceReader {
	led := &ledgerCache{}
	return func(ctx context.Context, q store.EvidenceQuery) store.Evidence {
		ev := store.Evidence{LedgerRepo: ledgerRepo, LedgerPath: ledgerPath}
		acc, err := access(ctx)
		if err != nil {
			ev.NotRead = append(ev.NotRead, "the repositories (no access to Gitea: "+oneLine(err.Error())+")")
			return ev
		}
		ev.Org = acc.Org
		repos := q.Repos
		if len(repos) > evidenceRepos {
			ev.NotRead = append(ev.NotRead, fmt.Sprintf("main of %s (only %d repositories are read)", strings.Join(repos[evidenceRepos:], ", "), evidenceRepos))
			repos = repos[:evidenceRepos]
		}
		var all []store.EvidenceCommit
		for _, repo := range repos {
			commits, cut, err := lineage.MainCommits(ctx, acc, client, repo, q.Since, evidencePages)
			if err != nil {
				ev.NotRead = append(ev.NotRead, "main of "+repo+" ("+oneLine(err.Error())+")")
			}
			if cut && len(commits) > 0 {
				// The pages ran out before the window's start (review 448 C2).
				ev.NotRead = append(ev.NotRead, fmt.Sprintf("main of %s before %s (only its latest %d commits are read)", repo,
					commits[len(commits)-1].At.UTC().Format(time.RFC3339), len(commits)))
			}
			for _, c := range commits {
				all = append(all, store.EvidenceCommit{Repo: repo, SHA: c.SHA, Subject: c.Subject, At: c.At, ChangeID: c.ChangeID, Task: c.Task, Work: c.Work})
			}
		}
		ev.Commits = tiedCommits(all, q)
		if ledgerRepo == "" {
			ev.NotRead = append(ev.NotRead, "the ledger (AGENTRT_LEDGER is not set on this platform)")
			return ev
		}
		ids := slices.Clone(q.Items)
		for _, c := range ev.Commits {
			for _, w := range c.Work {
				if !slices.Contains(ids, w) {
					ids = append(ids, w)
				}
			}
		}
		if len(ids) == 0 {
			return ev
		}
		body, err := led.read(ctx, func(ctx context.Context) ([]byte, error) {
			return lineage.RawFile(ctx, acc, client, ledgerRepo, "main", ledgerPath)
		})
		if err != nil {
			ev.NotRead = append(ev.NotRead, "the ledger "+ledgerRepo+"/"+ledgerPath+" ("+oneLine(err.Error())+")")
			return ev
		}
		ev.Ledger = ledgerItems(body, ids)
		return ev
	}
}

// tiedCommits is the commits that tie to the query, newest first: results of
// its tasks, the commits it names, and those whose Work trailer or subject
// names one of its items, unless the item is a programme that more than
// broadItem commits name.
func tiedCommits(all []store.EvidenceCommit, q store.EvidenceQuery) []store.EvidenceCommit {
	named := func(c store.EvidenceCommit) []string {
		var out []string
		for _, id := range append(slices.Clone(c.Work), itemsIn(c.Subject)...) {
			if slices.Contains(q.Items, id) && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		return out
	}
	count := map[string]int{}
	for _, c := range all {
		for _, id := range named(c) {
			count[id]++
		}
	}
	var out []store.EvidenceCommit
	for _, c := range all {
		tied := slices.Contains(q.Tasks, c.Task) && c.Task != ""
		for _, p := range q.Commits {
			tied = tied || (len(p) >= 7 && strings.HasPrefix(c.SHA, p))
		}
		for _, id := range named(c) {
			tied = tied || count[id] <= broadItem
		}
		if tied {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

var subjectItems = regexp.MustCompile(`\b[A-Z][A-Z0-9]{0,15}-[0-9][0-9A-Z]{0,7}\b`)

func itemsIn(s string) []string { return subjectItems.FindAllString(s, -1) }

// ledgerHeading is a ledger item's heading: "### TD-378".
var ledgerHeading = regexp.MustCompile(`^#{2,4}[ \t]+([A-Z][A-Z0-9]{0,15}-[0-9][0-9A-Z]{0,7})[ \t]*$`)

// ledgerItems reads the items ids names from a ledger: under each item's
// heading, its first line (the title) and its first list line (its state).
func ledgerItems(body []byte, ids []string) []store.LedgerItem {
	var out []store.LedgerItem
	var cur *store.LedgerItem
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := ledgerHeading.FindStringSubmatch(line); m != nil {
			cur = nil
			if slices.Contains(ids, m[1]) {
				out = append(out, store.LedgerItem{ID: m[1]})
				cur = &out[len(out)-1]
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			cur = nil
		}
		if cur == nil || line == "" {
			continue
		}
		plain := strings.ReplaceAll(line, "**", "")
		switch {
		case cur.Title == "":
			cur.Title = plain
		case cur.State == "" && strings.HasPrefix(line, "- "):
			cur.State = strings.TrimPrefix(plain, "- ")
		}
	}
	return out
}

// ledgerCache keeps the last good read of the ledger for ledgerTTL.
type ledgerCache struct {
	mu   sync.Mutex
	at   time.Time
	body []byte
}

func (l *ledgerCache) read(ctx context.Context, fetch func(context.Context) ([]byte, error)) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.body != nil && time.Since(l.at) < ledgerTTL {
		return l.body, nil
	}
	b, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	l.body, l.at = b, time.Now()
	return b, nil
}

// oneLine is an error's text on one line, short.
func oneLine(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 160 {
		r = r[:160]
	}
	return string(r)
}
