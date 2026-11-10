package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Wave records are controller notes on wave:<repo>. They describe work;
// recording them never changes a Git reference.
var ErrWaveRefused = errors.New("wave record refused")

var waveFields = map[string]string{
	"wave_opened":         "wave repo ref plan_root n opened_by base",
	"wave_appended":       "wave task commit parent change_id rebased",
	"wave_append_refused": "wave task why paths",
	"wave_closed":         "wave by",
	"wave_landing":        "wave batch M0 R",
	"wave_red":            "wave batch blamed prefix",
	"wave_landed":         "wave R main",
	"wave_abandoned":      "wave by why",
	"wave_tally":          "wave landing_ci stg prod",
}

// WaveMember preserves the append order and replay evidence.
type WaveMember struct {
	Task, Commit, Parent, ChangeID string
	Rebased                        bool
}

type WaveTally struct{ LandingCI, Stg, Prod int }

// WaveState is the current projection of one wave's notes.
type WaveState struct {
	Wave, Repo, Ref, PlanRoot, OpenedBy, Base string
	N                                         int
	State, Head                               string
	Members                                   []WaveMember
	Tallies                                   WaveTally
	Batch, M0, R, Main                        string
	Blamed, Prefix                            []string
}

func waveString(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func waveInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, n >= 0
	case int64:
		return int(n), n >= 0 && int64(int(n)) == n
	case float64:
		return int(n), n >= 0 && n == float64(int(n))
	}
	return 0, false
}

func badWave(name string, v any) string {
	m, ok := v.(map[string]any)
	fields := append([]string{"v"}, strings.Fields(waveFields[name])...)
	if !ok || !exactKeys(m, fields) || m["v"] != "1" {
		return name + " is a v1 object of " + strings.Join(fields, ", ")
	}
	for _, k := range fields[1:] {
		switch k {
		case "n", "landing_ci", "stg", "prod":
			n, ok := waveInt(m[k])
			if !ok || k == "n" && n == 0 {
				return k + " is a nonnegative integer (n is positive)"
			}
		case "rebased":
			if _, ok := m[k].(bool); !ok {
				return "rebased is boolean"
			}
		case "paths", "blamed", "prefix":
			l, ok := stringList(m[k])
			if !ok {
				return k + " is a string list"
			}
			for _, s := range l {
				if s == "" {
					return k + " contains an empty value"
				}
			}
		default:
			s, ok := m[k].(string)
			if !ok || strings.TrimSpace(s) == "" {
				return k + " is a nonempty string"
			}
			if k == "base" || k == "commit" || k == "parent" || k == "M0" || k == "R" || k == "main" {
				if !landingSHA.MatchString(s) {
					return k + " is a commit"
				}
			}
		}
	}
	if name == "wave_opened" {
		n, _ := waveInt(m["n"])
		if m["ref"] != fmt.Sprintf("refs/heads/wave/%s/%d", m["plan_root"], n) {
			return "ref names plan_root and n"
		}
	}
	if name == "wave_append_refused" && m["why"] != "excluded" && m["why"] != "conflict" {
		return "why is excluded or conflict"
	}
	return ""
}

// WaveStateOf folds repository notes in stream order, keyed by wave ID.
func WaveStateOf(ctx context.Context, tx pgx.Tx, repo string) (map[string]WaveState, error) {
	rows, err := tx.Query(ctx, `SELECT body_json FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note' ORDER BY seq`, "wave:"+repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]WaveState{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, err
		}
		for name := range waveFields {
			m, ok := body[name].(map[string]any)
			if !ok {
				continue
			}
			id := waveString(m, "wave")
			w := out[id]
			switch name {
			case "wave_opened":
				w = WaveState{Wave: id, Repo: waveString(m, "repo"), Ref: waveString(m, "ref"), PlanRoot: waveString(m, "plan_root"), OpenedBy: waveString(m, "opened_by"), Base: waveString(m, "base"), Head: waveString(m, "base"), State: "open"}
				w.N, _ = waveInt(m["n"])
			case "wave_appended":
				rebased, _ := m["rebased"].(bool)
				w.Members = append(w.Members, WaveMember{Task: waveString(m, "task"), Commit: waveString(m, "commit"), Parent: waveString(m, "parent"), ChangeID: waveString(m, "change_id"), Rebased: rebased})
				w.Head = waveString(m, "commit")
			case "wave_closed":
				w.State = "closing"
			case "wave_landing":
				w.State = "landing"
				w.Batch = waveString(m, "batch")
				w.M0 = waveString(m, "M0")
				w.R = waveString(m, "R")
			case "wave_red":
				w.State = "red"
				w.Blamed, _ = stringList(m["blamed"])
				w.Prefix, _ = stringList(m["prefix"])
			case "wave_landed":
				w.State = "landed"
				w.R = waveString(m, "R")
				w.Main = waveString(m, "main")
			case "wave_abandoned":
				w.State = "abandoned"
			case "wave_tally":
				w.Tallies.LandingCI, _ = waveInt(m["landing_ci"])
				w.Tallies.Stg, _ = waveInt(m["stg"])
				w.Tallies.Prod, _ = waveInt(m["prod"])
			}
			out[id] = w
		}
	}
	return out, rows.Err()
}

func checkWaveAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	for name := range waveFields {
		value, ok := ev.Body[name]
		if !ok {
			continue
		}
		refuse := func(why string) error { return fmt.Errorf("%w: %s", ErrWaveRefused, why) }
		if ev.Type != "note" || ev.Actor != "controller" || !strings.HasPrefix(ev.Stream, "wave:") || strings.TrimPrefix(ev.Stream, "wave:") == "" || !onlyBusinessKey(ev.Body, name) {
			return refuse("controller note on wave:<repo>, with this record alone")
		}
		repo := strings.TrimPrefix(ev.Stream, "wave:")
		states, err := WaveStateOf(ctx, tx, repo)
		if err != nil {
			return err
		}
		m := value.(map[string]any)
		id := waveString(m, "wave")
		w, exists := states[id]
		if name == "wave_opened" {
			if exists {
				return refuse("wave already exists")
			}
			if m["repo"] != repo {
				return refuse("repo differs from stream")
			}
			for _, other := range states {
				if other.State == "open" && other.Ref == m["ref"] {
					return refuse("ref already has an open wave")
				}
			}
			return nil
		}
		if !exists {
			return refuse("unknown wave")
		}
		switch name {
		case "wave_appended", "wave_append_refused", "wave_closed":
			if w.State != "open" {
				return refuse("wave is not open")
			}
			if name == "wave_appended" && m["parent"] != w.Head {
				return refuse("parent is not the wave head")
			}
		case "wave_landing":
			if w.State != "closing" && w.State != "red" {
				return refuse("wave is not closing or red")
			}
		case "wave_red", "wave_landed":
			if w.State != "landing" {
				return refuse("wave is not landing")
			}
			if name == "wave_landed" && m["R"] != w.R {
				return refuse("R differs from landing")
			}
			if name == "wave_red" && m["batch"] != w.Batch {
				return refuse("batch differs from landing")
			}
		case "wave_abandoned":
			if w.State == "landed" || w.State == "abandoned" {
				return refuse("wave has ended")
			}
		}
	}
	return nil
}

// waveBody keeps every record's wire version and fields fixed, including empty
// lists and false booleans.
func waveBody(v any) map[string]any {
	raw, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for k, value := range m {
		if value == nil {
			m[k] = []any{}
		}
	}
	m["v"] = "1"
	return m
}

// WaveOpened is the corresponding v1 wave note body.
type WaveOpened struct {
	Wave     string `json:"wave"`
	Repo     string `json:"repo"`
	Ref      string `json:"ref"`
	PlanRoot string `json:"plan_root"`
	N        int    `json:"n"`
	OpenedBy string `json:"opened_by"`
	Base     string `json:"base"`
}

func (b WaveOpened) body() map[string]any { return waveBody(b) }

// WaveAppended is the corresponding v1 wave note body.
type WaveAppended struct {
	Wave     string `json:"wave"`
	Task     string `json:"task"`
	Commit   string `json:"commit"`
	Parent   string `json:"parent"`
	ChangeID string `json:"change_id"`
	Rebased  bool   `json:"rebased"`
}

func (b WaveAppended) body() map[string]any { return waveBody(b) }

// WaveAppendRefused is the corresponding v1 wave note body.
type WaveAppendRefused struct {
	Wave  string   `json:"wave"`
	Task  string   `json:"task"`
	Why   string   `json:"why"`
	Paths []string `json:"paths"`
}

func (b WaveAppendRefused) body() map[string]any { return waveBody(b) }

// WaveClosed is the corresponding v1 wave note body.
type WaveClosed struct {
	Wave string `json:"wave"`
	By   string `json:"by"`
}

func (b WaveClosed) body() map[string]any { return waveBody(b) }

// WaveLanding is the corresponding v1 wave note body.
type WaveLanding struct {
	Wave  string `json:"wave"`
	Batch string `json:"batch"`
	M0    string `json:"M0"`
	R     string `json:"R"`
}

func (b WaveLanding) body() map[string]any { return waveBody(b) }

// WaveRed is the corresponding v1 wave note body.
type WaveRed struct {
	Wave   string   `json:"wave"`
	Batch  string   `json:"batch"`
	Blamed []string `json:"blamed"`
	Prefix []string `json:"prefix"`
}

func (b WaveRed) body() map[string]any { return waveBody(b) }

// WaveLanded is the corresponding v1 wave note body.
type WaveLanded struct {
	Wave string `json:"wave"`
	R    string `json:"R"`
	Main string `json:"main"`
}

func (b WaveLanded) body() map[string]any { return waveBody(b) }

// WaveAbandoned is the corresponding v1 wave note body.
type WaveAbandoned struct {
	Wave string `json:"wave"`
	By   string `json:"by"`
	Why  string `json:"why"`
}

func (b WaveAbandoned) body() map[string]any { return waveBody(b) }

// WaveTallyNote records cumulative counters for a wave.
type WaveTallyNote struct {
	Wave      string `json:"wave"`
	LandingCI int    `json:"landing_ci"`
	Stg       int    `json:"stg"`
	Prod      int    `json:"prod"`
}

func (b WaveTallyNote) body() map[string]any { return waveBody(b) }
