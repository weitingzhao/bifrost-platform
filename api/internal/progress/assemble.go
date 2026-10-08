package progress

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

const (
	defaultStuckDays = 3
	maxStuckDays     = 30
	scanDays         = 90
)

// Item is one registered work item with the progress computed from commits.
type Item struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Category        string     `json:"category"`
	Status          string     `json:"status"`
	AcceptResult    string     `json:"accept_result,omitempty"`
	Threads         []Thread   `json:"threads"`
	CommitCount     int        `json:"commit_count"`
	Environments    []string   `json:"environments"`
	LastActivity    *time.Time `json:"last_activity,omitempty"`
	AwaitingSignoff bool       `json:"awaiting_signoff"`
	Stuck           bool       `json:"stuck"`
	// Match is the extra ids (LANE-…) a commit may use. Not part of the response.
	Match string `json:"-"`
}

// Thread is one Claude-Session that touched the item.
type Thread struct {
	Session string `json:"session"`
}

// Summary is the four columns plus unassigned commits.
// awaiting_signoff = 待你签收, in_flight = 在途, shipped_this_week = 本周上线, stuck = 卡住.
type Summary struct {
	AwaitingSignoff   int `json:"awaiting_signoff"`
	InFlight          int `json:"in_flight"`
	ShippedThisWeek   int `json:"shipped_this_week"`
	Stuck             int `json:"stuck"`
	UnassignedCommits int `json:"unassigned_commits"`
}

// Response is GET /api/v1/progress.
type Response struct {
	GeneratedAt    time.Time `json:"generated_at"`
	StuckAfterDays int       `json:"stuck_after_days"`
	Summary        Summary   `json:"summary"`
	Items          []Item    `json:"items"`
	Errors         []string  `json:"errors,omitempty"`
}

func clampStuck(n int) int {
	if n <= 0 {
		return defaultStuckDays
	}
	if n > maxStuckDays {
		return maxStuckDays
	}
	return n
}

func weekStart(t time.Time) time.Time {
	t = t.UTC()
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	d := t.AddDate(0, 0, -(wd - 1))
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

func normEnv(env string) string {
	e := strings.ToLower(strings.TrimSpace(env))
	switch {
	case e == "prod" || e == "production" || strings.HasSuffix(e, "-prod"):
		return "PROD"
	case e == "stg" || e == "staging" || strings.HasSuffix(e, "-stg"):
		return "STG"
	default:
		return ""
	}
}

func workMissing(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || strings.EqualFold(v, "unassigned")
}

func commitIDs(c lineage.Commit) []string {
	var ids []string
	if !workMissing(c.Work) {
		ids = append(ids, findIDs(c.Work)...)
	}
	ids = append(ids, findIDs(c.Subject)...)
	return dedupe(ids)
}

func dedupe(ids []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func itemIDs(it Item) []string {
	return dedupe(append(append([]string{it.ID}, findIDs(it.Title)...), findIDs(it.Match)...))
}

func inFlight(status string) bool {
	switch statusKey(status) {
	case "未开始", "在做", "观察中":
		return true
	default:
		return false
	}
}

// Assemble joins fabricated or live documents with commits. It does not read the network.
func Assemble(now time.Time, stuckDays int, debt, work string, commits []lineage.Commit) Response {
	stuckDays = clampStuck(stuckDays)
	now = now.UTC()
	week := weekStart(now)
	stuckAfter := time.Duration(stuckDays) * 24 * time.Hour

	items := parseDocs(debt, work)
	keys := make([][]string, len(items))
	shipped := make([]bool, len(items))
	for i := range items {
		keys[i] = itemIDs(items[i])
		items[i].Threads = []Thread{}
		items[i].Environments = []string{}
		if statusKey(items[i].Status) == "待你签收" {
			items[i].AwaitingSignoff = true
		}
	}

	seenSHA := map[string]bool{}
	unassigned := 0
	for _, c := range commits {
		if c.SHA != "" {
			if seenSHA[c.SHA] {
				continue
			}
			seenSHA[c.SHA] = true
		}
		if workMissing(c.Work) {
			unassigned++
		}
		ids := commitIDs(c)
		if len(ids) == 0 {
			continue
		}
		idSet := map[string]bool{}
		for _, id := range ids {
			idSet[id] = true
		}
		for i := range items {
			if !overlap(keys[i], idSet) {
				continue
			}
			items[i].CommitCount++
			if items[i].LastActivity == nil || c.At.After(*items[i].LastActivity) {
				at := c.At.UTC()
				items[i].LastActivity = &at
			}
			if c.Session != "" && !hasSession(items[i].Threads, c.Session) {
				items[i].Threads = append(items[i].Threads, Thread{Session: c.Session})
			}
			for _, r := range c.Reached {
				env := normEnv(r.Env)
				if env == "" {
					continue
				}
				if !hasEnv(items[i].Environments, env) {
					items[i].Environments = append(items[i].Environments, env)
				}
				if env == "PROD" && !r.At.Before(week) && !r.At.After(now) {
					shipped[i] = true
				}
			}
		}
	}

	sum := Summary{UnassignedCommits: unassigned}
	for i := range items {
		items[i].Environments = orderEnvs(items[i].Environments)
		if items[i].AwaitingSignoff {
			sum.AwaitingSignoff++
		}
		if inFlight(items[i].Status) {
			sum.InFlight++
		}
		idle := items[i].LastActivity == nil || now.Sub(*items[i].LastActivity) >= stuckAfter
		if statusKey(items[i].Status) == "在做" && idle {
			items[i].Stuck = true
			sum.Stuck++
		}
		if shipped[i] {
			sum.ShippedThisWeek++
		}
		items[i].Match = ""
	}
	sort.SliceStable(items, func(i, j int) bool { return itemLess(items[i], items[j]) })
	return Response{
		GeneratedAt: now, StuckAfterDays: stuckDays, Summary: sum, Items: items,
	}
}

func overlap(keys []string, set map[string]bool) bool {
	for _, k := range keys {
		if set[k] {
			return true
		}
	}
	return false
}

func hasSession(ts []Thread, session string) bool {
	for _, t := range ts {
		if t.Session == session {
			return true
		}
	}
	return false
}

func orderEnvs(envs []string) []string {
	var stg, prod bool
	var rest []string
	for _, e := range envs {
		switch e {
		case "STG":
			stg = true
		case "PROD":
			prod = true
		default:
			rest = append(rest, e)
		}
	}
	sort.Strings(rest)
	out := make([]string, 0, len(envs))
	if stg {
		out = append(out, "STG")
	}
	if prod {
		out = append(out, "PROD")
	}
	return append(out, rest...)
}

func hasEnv(envs []string, env string) bool {
	for _, e := range envs {
		if e == env {
			return true
		}
	}
	return false
}

func catOrder(c string) int {
	switch c {
	case catDebt:
		return 0
	case catPlan:
		return 1
	case catLane:
		return 2
	default:
		return 3
	}
}

func numSuffix(id string) int {
	i := strings.LastIndexAny(id, "-")
	if i < 0 {
		return 0
	}
	n, _ := strconv.Atoi(id[i+1:])
	return n
}

func itemLess(a, b Item) bool {
	if ca, cb := catOrder(a.Category), catOrder(b.Category); ca != cb {
		return ca < cb
	}
	if na, nb := numSuffix(a.ID), numSuffix(b.ID); na != nb {
		return na < nb
	}
	return a.ID < b.ID
}
