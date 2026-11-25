package catalog

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"sort"
	"time"
)

// Daily readings (2026-11-24, the map line's completion criteria in the
// plan, R6:6.9): three of them could only be read by hand at the moment
// someone asked, so nothing could say they held from one day to the next:
// whether the read routes are read every day (perceive first 5), how long a
// new commit takes to be on the map (perceive first 6), and whether the
// pool's oldest entry only gets younger (LAW-10). Each round keeps today's
// point, one per UTC day, the last DailyDays of them; the status carries
// them. They measure; they refuse nothing.

// DailyDays is how many days of points are kept.
const DailyDays = 90

// ReadRoutes are the map's read routes whose daily calls are kept.
var ReadRoutes = []string{"/api/v1/catalog/status", "/api/v1/catalog/domain", "/api/v1/catalog/owner",
	"/api/v1/catalog/unperceived", "/api/v1/catalog/index", "/api/v1/catalog/users"}

const keptDaily = "daily"
const keptSeen = "seen"

// SeenMax is how many sightings of new main commits are kept.
const SeenMax = 200

// Sighting is one new main commit of a repository and the round that first
// read it: LagSeconds is from the commit's time (the landing writes it) to
// that round.
type Sighting struct {
	Repo       string    `json:"repo"`
	Commit     string    `json:"commit"`
	Committed  time.Time `json:"committed"`
	Seen       time.Time `json:"seen"`
	LagSeconds int64     `json:"lag_seconds"`
}

// DailyPoint is one UTC day as the last round of that day read it.
type DailyPoint struct {
	Date string    `json:"date"`
	At   time.Time `json:"at"`
	// Reads are each read route's calls over the 24 hours before At; nil
	// when the counts could not be read.
	Reads map[string]int `json:"reads"`
	// OldestDays is each repository's oldest pool entry in days (absent:
	// an empty pool); MaxOldestDays the largest of them.
	OldestDays    map[string]int `json:"oldest_days"`
	MaxOldestDays int            `json:"max_oldest_days"`
	// Seen counts the new main commits first read that day, MaxLagSeconds
	// the longest wait; RoundSeconds is one round, the bar is two.
	Seen          int   `json:"seen"`
	MaxLagSeconds int64 `json:"max_lag_seconds"`
	RoundSeconds  int64 `json:"round_seconds"`
}

// noteSighting keeps a new main commit of repo when the commit's time can be
// read (Scanner.CommitTime). Called with the scanner's lock held.
func (s *Scanner) noteSighting(ctx context.Context, repo, before, commit string, now time.Time) {
	if s.CommitTime == nil || before == "" || commit == "" || before == commit {
		return
	}
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	at, err := s.CommitTime(c, repo, commit)
	cancel()
	if err != nil || at.IsZero() {
		return
	}
	var list []Sighting
	if raw, err := s.kept().Read(keptSeen); err == nil && raw != nil {
		_ = json.Unmarshal(raw, &list)
	}
	lag := int64(now.Sub(at) / time.Second)
	if lag < 0 {
		lag = 0
	}
	list = append(list, Sighting{Repo: repo, Commit: commit, Committed: at.UTC(), Seen: now.UTC(), LagSeconds: lag})
	if len(list) > SeenMax {
		list = list[len(list)-SeenMax:]
	}
	if raw, err := json.Marshal(list); err == nil {
		if err := s.kept().Write(keptSeen, raw); err != nil {
			slog.Warn("catalog: keep sightings", "err", err)
		}
	}
}

// keepDaily writes today's point and returns the kept days, oldest first.
func (s *Scanner) keepDaily(ctx context.Context, in StatusInputs, now time.Time) []DailyPoint {
	var days []DailyPoint
	if raw, err := s.kept().Read(keptDaily); err == nil && raw != nil {
		_ = json.Unmarshal(raw, &days)
	}
	date := now.UTC().Format("2006-01-02")
	p := DailyPoint{Date: date, At: now.UTC(), OldestDays: map[string]int{}, RoundSeconds: int64(Every / time.Second)}
	if in.Requests != nil {
		if counts, err := in.Requests(ctx, ReadRoutes, 24*time.Hour); err == nil {
			p.Reads = map[string]int{}
			for _, r := range ReadRoutes {
				p.Reads[r] = int(math.Round(counts[r].Total))
			}
		}
	}
	var repos []string
	if raw, err := s.kept().Read(keptRepos); err == nil && raw != nil {
		_ = json.Unmarshal(raw, &repos)
	}
	for _, r := range repos {
		if pool, _ := s.Last(r); pool != nil && pool.OldestDays != nil {
			p.OldestDays[r] = *pool.OldestDays
			if *pool.OldestDays > p.MaxOldestDays {
				p.MaxOldestDays = *pool.OldestDays
			}
		}
	}
	var seen []Sighting
	if raw, err := s.kept().Read(keptSeen); err == nil && raw != nil {
		_ = json.Unmarshal(raw, &seen)
	}
	for _, x := range seen {
		if x.Seen.UTC().Format("2006-01-02") == date {
			p.Seen++
			if x.LagSeconds > p.MaxLagSeconds {
				p.MaxLagSeconds = x.LagSeconds
			}
		}
	}
	kept := days[:0:0]
	for _, d := range days {
		if d.Date != date {
			kept = append(kept, d)
		}
	}
	kept = append(kept, p)
	sort.Slice(kept, func(i, j int) bool { return kept[i].Date < kept[j].Date })
	if len(kept) > DailyDays {
		kept = kept[len(kept)-DailyDays:]
	}
	if raw, err := json.Marshal(kept); err == nil {
		if err := s.kept().Write(keptDaily, raw); err != nil {
			slog.Warn("catalog: keep daily readings", "err", err)
		}
	}
	return kept
}
