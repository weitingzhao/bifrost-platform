// Package nextgen is the communication layer's own record of its change of
// generation: every channel of the old layer and where it goes
// (channels.yaml), the platform readers moved off the cluster and Gitea
// (readers.yaml), and the baselines its readings are compared with
// (baselines.yaml). The Bus page shows them; nothing on it is typed in by
// hand beyond these files, and their tests hold them to the code (the Owner,
// 2026-11-17: nothing in Ops may be kept by hand; what it shows is computed
// from the source).
package nextgen

import (
	_ "embed"
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed channels.yaml
var channelsYAML []byte

//go:embed readers.yaml
var readersYAML []byte

//go:embed baselines.yaml
var baselinesYAML []byte

// Target is how many channels the next generation keeps: kept 31 + merged
// 20 + its 13 new pieces (design v1.2).
const Target = 64

// Retired says an old channel is gone and what proves it.
type Retired struct {
	At         string   `yaml:"at" json:"at"`
	Change     string   `yaml:"change" json:"change"`
	Absent     []string `yaml:"absent" json:"absent,omitempty"`
	AbsentText []string `yaml:"absent_text" json:"absent_text,omitempty"`
	Outside    string   `yaml:"outside" json:"outside,omitempty"`
}

// Channel is one row of the disposition table.
type Channel struct {
	ID          string   `yaml:"id" json:"id"`
	Class       string   `yaml:"class" json:"class"`
	Disposition string   `yaml:"disposition" json:"disposition"`
	What        string   `yaml:"what" json:"what"`
	By          string   `yaml:"by" json:"by"`
	Retired     *Retired `yaml:"retired" json:"retired,omitempty"`
}

// Proof is the code a moved reader stands on.
type Proof struct {
	File     string `yaml:"file" json:"file"`
	Contains string `yaml:"contains" json:"contains"`
}

// Reader is one reader of the source switch.
type Reader struct {
	ID     string `yaml:"id" json:"id"`
	What   string `yaml:"what" json:"what"`
	Status string `yaml:"status" json:"status"`
	At     string `yaml:"at" json:"at,omitempty"`
	Owner  string `yaml:"owner" json:"owner,omitempty"`
	Note   string `yaml:"note" json:"note,omitempty"`
	Proof  *Proof `yaml:"proof" json:"proof,omitempty"`
	// WatchRepo: the reader moves when gate-git watches this repository
	// (BUS_GIT_REPOS); whether it does is read when asked, not written here.
	WatchRepo  string `yaml:"watch_repo" json:"watch_repo,omitempty"`
	AbsentText string `yaml:"absent_text" json:"absent_text,omitempty"`
	// Evidence is whether a moved reader runs on its new source here; read
	// from the running system when asked, never written in the file.
	Evidence *Evidence `yaml:"-" json:"evidence,omitempty"`
}

// Evidence states of a moved reader.
const (
	EvidenceRunning    = "running"    // answered from its new source, as recently as it usually does
	EvidenceStale      = "stale"      // it did, but has been silent far longer than its own pace
	EvidenceNone       = "none"       // no answer from its new source in the window
	EvidenceUnmeasured = "unmeasured" // the readings could not be read
)

// EvidenceWindow is how far back the readings are taken: the window the
// display routes earn their trust over (EarnedRoutes' readings).
const EvidenceWindow = 7 * 24 * time.Hour

// A reader is stale after evidenceGaps of its own average gaps without an
// answer, never sooner than evidenceFloor (a scrape or two of slack).
const (
	evidenceGaps  = 5
	evidenceFloor = 15 * time.Minute
)

// Evidence is a moved reader's run-time proof in this environment.
type Evidence struct {
	State    string     `json:"state"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
	Runs     float64    `json:"runs_7d"`
	// StaleAfterSeconds is how long this reader may be silent, from its own pace.
	StaleAfterSeconds float64 `json:"stale_after_seconds,omitempty"`
}

// JudgeEvidence reads one reader: last is the Unix time of its last answer
// from the new source (0 for none), runs how many it gave in EvidenceWindow.
// How long it may be silent comes from its own pace: a 30-second loop is
// missed within minutes, a page opened twice a week only after days.
func JudgeEvidence(now time.Time, last, runs float64) Evidence {
	e := Evidence{State: EvidenceNone, Runs: runs}
	if last <= 0 {
		return e
	}
	at := time.Unix(int64(last), 0).UTC()
	e.LastSeen = &at
	age := now.Sub(at)
	if age > EvidenceWindow {
		return e
	}
	allow := EvidenceWindow
	if runs >= 1 {
		allow = time.Duration(float64(EvidenceWindow) / runs * evidenceGaps)
	}
	allow = max(evidenceFloor, min(allow, EvidenceWindow))
	e.StaleAfterSeconds = allow.Seconds()
	e.State = EvidenceRunning
	if age > allow {
		e.State = EvidenceStale
	}
	return e
}

// ReaderGroup is a kind of reader.
type ReaderGroup struct {
	ID      string   `yaml:"id" json:"id"`
	Title   string   `yaml:"title" json:"title"`
	Readers []Reader `yaml:"readers" json:"readers"`
}

// Baseline is a reading taken by the system at a time: the query and the
// time are written here, the value is computed from them when asked.
type Baseline struct {
	Key   string `yaml:"key" json:"key"`
	Label string `yaml:"label" json:"label"`
	At    string `yaml:"at" json:"at"`
	Query string `yaml:"query" json:"query"`
	Note  string `yaml:"note" json:"note,omitempty"`
}

// Dispositions in the table, in the order the page shows them.
var Dispositions = []string{"replaced", "merged", "deleted", "kept", "announced", "not-this-way", "reserved"}

// Reader statuses.
const (
	StatusMoved   = "moved"
	StatusRetired = "retired"
	StatusKept    = "kept"
	StatusOpen    = "open"
)

// Channels reads the disposition table.
func Channels() ([]Channel, error) {
	var doc struct {
		Channels []Channel `yaml:"channels"`
	}
	if err := yaml.Unmarshal(channelsYAML, &doc); err != nil {
		return nil, fmt.Errorf("nextgen: channels.yaml: %w", err)
	}
	return doc.Channels, nil
}

// Readers reads the source switch.
func Readers() ([]ReaderGroup, error) {
	var doc struct {
		Groups []ReaderGroup `yaml:"groups"`
	}
	if err := yaml.Unmarshal(readersYAML, &doc); err != nil {
		return nil, fmt.Errorf("nextgen: readers.yaml: %w", err)
	}
	return doc.Groups, nil
}

// Baselines reads the baselines.
func Baselines() ([]Baseline, error) {
	var doc struct {
		Baselines []Baseline `yaml:"baselines"`
	}
	if err := yaml.Unmarshal(baselinesYAML, &doc); err != nil {
		return nil, fmt.Errorf("nextgen: baselines.yaml: %w", err)
	}
	return doc.Baselines, nil
}

// Count is how many of a disposition there are and how many are retired.
type Count struct {
	Disposition string `json:"disposition"`
	Total       int    `json:"total"`
	Retired     int    `json:"retired"`
}

// Progress is the change of generation in numbers.
type Progress struct {
	Start   int       `json:"start"`
	Now     int       `json:"now"`
	Target  int       `json:"target"`
	Retired int       `json:"retired"`
	ToGo    int       `json:"to_go"`
	Counts  []Count   `json:"counts"`
	Recent  []Channel `json:"recent"`
}

// Summarize counts the table: now is the old channels not yet retired.
func Summarize(chs []Channel) Progress {
	p := Progress{Start: len(chs), Target: Target}
	by := map[string]*Count{}
	for _, d := range Dispositions {
		by[d] = &Count{Disposition: d}
	}
	var retired []Channel
	for _, c := range chs {
		ct, ok := by[c.Disposition]
		if !ok {
			ct = &Count{Disposition: c.Disposition}
			by[c.Disposition] = ct
		}
		ct.Total++
		if c.Retired != nil {
			ct.Retired++
			p.Retired++
			retired = append(retired, c)
		}
	}
	p.Now = p.Start - p.Retired
	if p.ToGo = p.Now - p.Target; p.ToGo < 0 {
		p.ToGo = 0
	}
	for _, d := range Dispositions {
		p.Counts = append(p.Counts, *by[d])
	}
	sort.SliceStable(retired, func(i, j int) bool { return retired[i].Retired.At > retired[j].Retired.At })
	if len(retired) > 10 {
		retired = retired[:10]
	}
	p.Recent = retired
	return p
}

// EarnedRoutes are the display routes that start trusted: n or more clean
// comparisons with the list and no difference in the readings given
// (bifrost_bus_readers_compared/mismatch over the last days, by route).
func EarnedRoutes(routes []string, compared, mismatch map[string]float64, n int) map[string]bool {
	out := map[string]bool{}
	for _, r := range routes {
		if compared[r] >= float64(n) && mismatch[r] == 0 {
			out[r] = true
		}
	}
	return out
}
