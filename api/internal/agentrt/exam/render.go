package exam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Rendering an item: its records with the time placeholders filled from t0,
// into the spec its production path writes (the set's README 「时间占位符」).
// Times are relative to t0 so a spec's clock agrees with the exam's.

var (
	offsetRE = regexp.MustCompile(`\{\{t0([+-]\d+[dhm](?:[+-]?\d+[dhm])*)\}\}`)
	partRE   = regexp.MustCompile(`([+-]?)(\d+)([dhm])`)
	missionN = regexp.MustCompile(`\bM-(\d+)\b`)
)

// at is t0 moved by spec: +2d, -6h05m, -29d13h, -30h+3m (a sign holds until
// the next one).
func at(t0 time.Time, spec string) (time.Time, error) {
	if spec == "" {
		return t0, nil
	}
	if !partRE.MatchString(spec) || partRE.ReplaceAllString(spec, "") != "" {
		return t0, fmt.Errorf("exam: %q is not an offset", spec)
	}
	t, sign := t0, "+"
	for _, m := range partRE.FindAllStringSubmatch(spec, -1) {
		if m[1] != "" {
			sign = m[1]
		}
		n, _ := strconv.Atoi(m[2])
		d := map[string]time.Duration{"d": 24 * time.Hour, "h": time.Hour, "m": time.Minute}[m[3]] * time.Duration(n)
		if sign == "-" {
			d = -d
		}
		t = t.Add(d)
	}
	return t, nil
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// fill replaces the placeholders in v (decoded JSON), common standing for
// {{snapshot_common…}}.
func fill(v any, t0 time.Time, common any) (any, error) {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "{{snapshot_common") {
			if common == nil {
				return nil, fmt.Errorf("exam: %s but the set has no snapshot_common", x)
			}
			return fill(common, t0, nil)
		}
		if x == "{{scheduled_note}}" {
			note, ok := store.ScheduledNote(t0)
			if !ok {
				return nil, fmt.Errorf("exam: %s is not a brief slot", stamp(t0))
			}
			return note, nil
		}
		var err error
		x = offsetRE.ReplaceAllStringFunc(x, func(m string) string {
			t, e := at(t0, offsetRE.FindStringSubmatch(m)[1])
			if e != nil {
				err = e
			}
			return stamp(t)
		})
		x = strings.ReplaceAll(x, "{{t0}}", stamp(t0))
		if err == nil && strings.Contains(x, "{{") {
			err = fmt.Errorf("exam: a placeholder is left in %q", x)
		}
		return x, err
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			f, err := fill(e, t0, common)
			if err != nil {
				return nil, err
			}
			out[i] = f
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			f, err := fill(e, t0, common)
			if err != nil {
				return nil, err
			}
			out[k] = f
		}
		return out, nil
	}
	return v, nil
}

func decode(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}

// records is an item's records with the placeholders filled.
func (s *Set) records(it *Item, t0 time.Time) (map[string]any, error) {
	in, err := decode(it.Input)
	if err != nil {
		return nil, err
	}
	common, err := decode(s.common())
	if err != nil {
		return nil, err
	}
	f, err := fill(in, t0, common)
	if err != nil {
		return nil, err
	}
	m, ok := f.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("exam: item %s's input is not an object", it.ID)
	}
	return m, nil
}

// T0 is the time an item is rendered at for a clock reading now: a scheduled
// brief's slot (the latest 08:00 or 21:00 at or before now in the Owner's
// zone), else now to the minute.
func T0(it *Item, now time.Time) time.Time {
	if it.Delivery().Path != "controller_note" {
		return now.UTC().Truncate(time.Minute)
	}
	loc, err := time.LoadLocation(store.BriefZone)
	if err != nil {
		return now.UTC().Truncate(time.Minute)
	}
	t := now.In(loc)
	for _, day := range []int{0, -1} {
		d := t.AddDate(0, 0, day)
		for _, h := range []int{21, 8} {
			slot := time.Date(d.Year(), d.Month(), d.Day(), h, 0, 0, 0, loc)
			if !slot.After(t) {
				return slot.UTC()
			}
		}
	}
	return now.UTC().Truncate(time.Minute)
}

// Paper is one item rendered: the spec the seat reads and, for a plan, the
// repository it reads at a commit.
type Paper struct {
	Item       string    `json:"item"`
	T0         time.Time `json:"t0"`
	Spec       []byte    `json:"spec"`
	Repo       string    `json:"repo,omitempty"`
	BaseCommit string    `json:"base_commit,omitempty"`
}

// Render renders item id at t0.
func (s *Set) Render(id string, t0 time.Time) (Paper, error) {
	it := s.items[id]
	if it == nil {
		return Paper{}, fmt.Errorf("exam: no item %s", id)
	}
	rec, err := s.records(it, t0)
	if err != nil {
		return Paper{}, err
	}
	p := Paper{Item: id, T0: t0}
	d := it.Delivery()
	if !T0(it, t0).Equal(t0) {
		// every time is relative to t0: off its slot or its minute, the
		// records would not agree with the note
		return Paper{}, fmt.Errorf("exam: item %s renders at %s, not %s", id, T0(it, t0).Format(time.RFC3339), t0.Format(time.RFC3339))
	}
	switch d.Path {
	case "controller_note":
		p.Spec, err = scheduledSpec(rec)
	case "mailbox":
		p.Spec, err = mailSpec(rec, t0)
	case "goal":
		if len(d.Repos) == 0 || d.BaseCommit == "" {
			return Paper{}, fmt.Errorf("exam: item %s goes to a planner with no repository or commit", id)
		}
		p.Repo, p.BaseCommit = d.Repos[0], d.BaseCommit
		p.Spec, err = goalSpec(rec, d, t0)
	default:
		err = fmt.Errorf("exam: item %s: unknown path %q", id, d.Path)
	}
	return p, err
}

// scheduledSpec is a scheduled brief's spec: the data as controllerBrief
// builds it, the pools as the store's PoolView (field order as at work).
func scheduledSpec(rec map[string]any) ([]byte, error) {
	data := map[string]any{}
	for k, v := range rec {
		data[k] = v
	}
	var pools []store.PoolView
	if err := roundTrip(rec["vendor_pools"], &pools); err != nil {
		return nil, fmt.Errorf("exam: vendor_pools: %w", err)
	}
	data["vendor_pools"] = pools
	for _, k := range []string{"done_since", "rejected_since", "stuck", "waiting_on_the_owner"} {
		var list []map[string]string
		if err := roundTrip(rec[k], &list); err != nil {
			return nil, fmt.Errorf("exam: %s: %w", k, err)
		}
		if list == nil {
			list = []map[string]string{}
		}
		data[k] = list
	}
	// The waits past their limits (W-31 2.3 slice 6), a list of its own: a
	// set's record of one may carry numbers (limit_s, waited_s), which the
	// lists above, strings only, would refuse.
	var waits []map[string]any
	if err := roundTrip(rec["waits_passed_since"], &waits); err != nil {
		return nil, fmt.Errorf("exam: waits_passed_since: %w", err)
	}
	if waits == nil {
		waits = []map[string]any{}
	}
	data["waits_passed_since"] = waits
	return store.ScheduledBriefSpecFor(data)
}

func roundTrip(v any, into any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

// snapRecords is a snapshot as the set writes it.
type snapRecords struct {
	Taken    string `json:"taken"`
	Missions []struct {
		Mission, State, Title, Goal string
	} `json:"missions_open"`
	Tasks []struct {
		Mission, Task, Role, State, Title string
	} `json:"tasks_open"`
	Decisions []struct {
		At, Mission, Decision, Why, Dropped, Reverse string
	} `json:"decisions"`
	Envelope string         `json:"envelope"`
	Budget   map[string]any `json:"budget"`
}

// snapshot renders the set's snapshot records for mission.
func snapshot(v any, mission string) (string, error) {
	var r snapRecords
	if err := roundTrip(v, &r); err != nil {
		return "", fmt.Errorf("exam: the snapshot: %w", err)
	}
	taken, err := time.Parse(time.RFC3339, r.Taken)
	if err != nil {
		return "", fmt.Errorf("exam: the snapshot's taken: %w", err)
	}
	s := store.Snapshot{Taken: taken, Envelope: r.Envelope, Mission: mission, Budget: pgJSON(r.Budget)}
	for _, m := range r.Missions {
		s.Missions = append(s.Missions, store.SnapshotMission{ID: m.Mission, State: m.State, Title: m.Title, Goal: m.Goal})
	}
	for _, t := range r.Tasks {
		s.Tasks = append(s.Tasks, store.SnapshotTask{Mission: t.Mission, ID: t.Task, Role: t.Role, State: t.State, Title: t.Title})
	}
	for _, d := range r.Decisions {
		when, err := time.Parse(time.RFC3339, d.At)
		if err != nil {
			return "", fmt.Errorf("exam: a decision's at: %w", err)
		}
		s.Decisions = append(s.Decisions, store.SnapshotDecision{At: when, Mission: d.Mission, Decision: d.Decision, Why: d.Why,
			Dropped: d.Dropped, Reverse: d.Reverse})
	}
	return store.RenderSnapshot(s), nil
}

// pgJSON writes a flat object as PostgreSQL writes jsonb: keys shorter first,
// then by bytes; ", " and ": " between.
func pgJSON(m map[string]any) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.SortFunc(ks, func(a, b string) int {
		if len(a) != len(b) {
			return len(a) - len(b)
		}
		return strings.Compare(a, b)
	})
	var b strings.Builder
	b.WriteString("{")
	for i, k := range ks {
		if i > 0 {
			b.WriteString(", ")
		}
		kj, _ := json.Marshal(k)
		vj, _ := json.Marshal(m[k])
		b.Write(kj)
		b.WriteString(": ")
		b.Write(vj)
	}
	b.WriteString("}")
	return b.String()
}

// ownMission is the mission a brief or a plan of these records would get: the
// one the records name, else the next number after the records' highest.
func ownMission(rec map[string]any) string {
	if m, ok := rec["mission"].(string); ok && m != "" {
		return m
	}
	raw, _ := json.Marshal(rec)
	top := 0
	for _, m := range missionN.FindAllStringSubmatch(string(raw), -1) {
		if n, _ := strconv.Atoi(m[1]); n > top {
			top = n
		}
	}
	return fmt.Sprintf("M-%d", top+1)
}

// mailSpec is the spec of a brief that answers the records' message.
func mailSpec(rec map[string]any, t0 time.Time) ([]byte, error) {
	var msg struct{ From, Kind, Body string }
	if err := roundTrip(rec["message"], &msg); err != nil || msg.From == "" || msg.Body == "" {
		return nil, fmt.Errorf("exam: the message: %v", err)
	}
	thread := []store.ThreadLine{}
	if err := roundTrip(rec["conversation_before"], &thread); err != nil {
		return nil, fmt.Errorf("exam: conversation_before: %w", err)
	}
	if thread == nil {
		thread = []store.ThreadLine{}
	}
	snap, err := snapshot(rec["runtime_now"], ownMission(rec))
	if err != nil {
		return nil, err
	}
	return store.BriefSpecFor(msg.From, msg.Kind, msg.Body, thread, snap)
}

// goalSpec is the spec of the planning task of the records' goal.
func goalSpec(rec map[string]any, d Delivery, t0 time.Time) ([]byte, error) {
	var g struct{ Title, Text string }
	if err := roundTrip(rec["goal"], &g); err != nil || g.Title == "" || g.Text == "" {
		return nil, fmt.Errorf("exam: the goal: %v", err)
	}
	mission := ownMission(rec)
	snap, err := snapshot(rec["snapshot"], mission)
	if err != nil {
		return nil, err
	}
	bases := map[string]string{}
	for _, r := range d.Repos {
		bases[r] = d.BaseCommit
	}
	return store.PlanSpecFor(store.Goal{Title: g.Title, Text: g.Text, Repos: d.Repos}, d.Repos, bases, mission, snap)
}
