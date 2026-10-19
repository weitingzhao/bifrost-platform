// Package exam is the chief of staff's qualifying exam (W-31 2.4 ④; the
// exam's cut, infra agent-config/work/multi-agent/CUT-chief-exam-2026-10-19.md):
// a question set of fixed records, rendered into the very specs the seat reads
// at work, and a deterministic grader of the answers. No model grades: every
// rule is a function of the answer and the records (design v5 §5.1.2).
//
// The set itself (questions, rules, key points, the controls) is not in this
// repository: a repository the runtime can check out is one an examinee could
// read (Owner's coordinating thread 2026-10-19). Load reads it from wherever
// the platform keeps it.
package exam

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Set is a loaded question set.
type Set struct {
	SHA256    string // over the question files, in name order
	Questions []*Question
	items     map[string]*Item
}

// Question is one question of the set (v4 §5.3 ①–⑤).
type Question struct {
	Exam     string          `json:"exam"`
	ID       string          `json:"question"`
	Version  int             `json:"version"`
	Class    string          `json:"class"`
	Title    string          `json:"title"`
	Delivery Delivery        `json:"delivery"`
	Common   json.RawMessage `json:"snapshot_common"`
	Items    []*Item         `json:"items"`
}

// Delivery is how an item is rendered: through which production path.
type Delivery struct {
	Path       string   `json:"path"` // controller_note, mailbox, goal
	Repos      []string `json:"repos"`
	BaseCommit string   `json:"base_commit"`
}

// Item is one sub-question: its records, its rules.
type Item struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Override *Delivery       `json:"delivery_override"`
	Input    json.RawMessage `json:"input"`
	Rules    []*Rule         `json:"rules"`
	question *Question
}

// Delivery is the item's delivery: its own when it has one.
func (it *Item) Delivery() Delivery {
	if it.Override != nil {
		return *it.Override
	}
	return it.question.Delivery
}

// Question is the question the item belongs to.
func (it *Item) Question() *Question { return it.question }

// Rule is one rule as the set writes it; the fields a kind does not use are
// empty. Patterns are compiled at load.
type Rule struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Hard *bool  `json:"hard"`

	N             int         `json:"n"`
	Items         []aliasItem `json:"items"`
	Patterns      []string    `json:"patterns"`
	Aliases       []string    `json:"aliases"`
	Forbid        []string    `json:"forbid"`
	StopAt        []string    `json:"stop_at"`
	HeadingForbid []string    `json:"heading_forbid"`
	GuardExtra    []string    `json:"guard_extra"`
	AnyOf         []string    `json:"any_of"`
	Window        *int        `json:"window"`
	Scope         string      `json:"scope"`
	ExpectPoints  float64     `json:"expect_points"`
	TolPoints     float64     `json:"tol_points"`
	At            string      `json:"at"`
	Zone          string      `json:"zone"`
	Expect        *bool       `json:"expect"`
	MustReference []string    `json:"must_reference_any"`
	Prefix        string      `json:"prefix"`
	MustCover     []coverItem `json:"must_cover"`
	Repos         []string    `json:"repos"`
	Groups        [][]string  `json:"groups"`
	Min           float64     `json:"min"`
	// the card
	SelectAnyOf         []string    `json:"select_any_of"`
	OwnerDuty           string      `json:"owner_duty"`
	OptionsMin          int         `json:"options_min"`
	OptionsMax          int         `json:"options_max"`
	OptionParse         optionParse `json:"option_parse"`
	Scores              []score     `json:"scores"`
	NoScoreExecutors    []string    `json:"no_score_executors"`
	MeanwhileForbid     []string    `json:"meanwhile_forbid"`
	MeanwhileGuardExtra []string    `json:"meanwhile_guard_extra"`

	re map[string]*regexp.Regexp // every pattern above, compiled
}

type aliasItem struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

type coverItem struct {
	Name  string   `json:"name"`
	AnyOf []string `json:"any_of"`
}

type optionParse struct {
	Threshold     string `json:"threshold"`
	MinSamples    string `json:"min_samples"`
	ExcludedAfter string `json:"excluded_after"`
	LimitedAfter  string `json:"limited_after"`
	Pair          string `json:"pair"`
	Empty         string `json:"empty"`
}

type score struct {
	Pair    string  `json:"pair"`
	Score   float64 `json:"score"`
	Samples int     `json:"samples"`
}

// IsHard says whether a failed run of the rule fails the item's run.
func (r *Rule) IsHard() bool { return r.Hard == nil || *r.Hard }

// kinds is the closed vocabulary of rule kinds (the set's README).
var kinds = map[string]bool{"max_lines": true, "mentions_all": true, "known_ids_only": true, "clause_forbid": true,
	"clause_require": true, "clause_each_require": true, "pace_near": true, "time_near": true, "require_any": true,
	"forbid_any": true, "alert": true, "protocol_lines": true, "plan_parses": true, "card": true, "section_order": true,
	"cjk_ratio": true}

// Load reads a set: every q*.json file at the root of fsys. A rule of an
// unknown kind, a pattern RE2 does not take, or two items of one id refuse
// the whole set.
func Load(fsys fs.FS) (*Set, error) {
	names, err := fs.Glob(fsys, "q*.json")
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("exam: no q*.json in the set")
	}
	slices.Sort(names)
	h := sha256.New()
	s := &Set{items: map[string]*Item{}}
	for _, name := range names {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", path.Base(name), len(raw))
		h.Write(raw)
		var q Question
		if err = json.Unmarshal(raw, &q); err != nil {
			return nil, fmt.Errorf("exam: %s: %w", name, err)
		}
		if q.ID == "" || len(q.Items) == 0 {
			return nil, fmt.Errorf("exam: %s: a question has an id and items", name)
		}
		for _, it := range q.Items {
			if it.ID == "" || s.items[it.ID] != nil {
				return nil, fmt.Errorf("exam: %s: item %q has no id or comes twice", name, it.ID)
			}
			it.question = &q
			if len(it.Rules) == 0 {
				return nil, fmt.Errorf("exam: %s: item %s has no rule", name, it.ID)
			}
			for _, r := range it.Rules {
				if err = r.compile(); err != nil {
					return nil, fmt.Errorf("exam: %s: item %s rule %s: %w", name, it.ID, r.ID, err)
				}
			}
			s.items[it.ID] = it
		}
		s.Questions = append(s.Questions, &q)
	}
	s.SHA256 = hex.EncodeToString(h.Sum(nil))
	return s, nil
}

// Item is the item of id, or nil.
func (s *Set) Item(id string) *Item { return s.items[id] }

// common is the records a {{snapshot_common…}} placeholder stands for: the
// first question that has them.
func (s *Set) common() json.RawMessage {
	for _, q := range s.Questions {
		if len(q.Common) > 0 {
			return q.Common
		}
	}
	return nil
}

func (r *Rule) compile() error {
	if !kinds[r.Kind] {
		return fmt.Errorf("unknown kind %q", r.Kind)
	}
	if r.ID == "" {
		return fmt.Errorf("a rule has an id")
	}
	r.re = map[string]*regexp.Regexp{}
	all := [][]string{r.Patterns, r.Forbid, r.HeadingForbid, r.AnyOf, r.SelectAnyOf, r.MeanwhileForbid}
	for _, c := range r.MustCover {
		all = append(all, c.AnyOf)
	}
	op := r.OptionParse
	all = append(all, []string{op.Threshold, op.MinSamples, op.Pair, op.Empty})
	for _, list := range all {
		for _, p := range list {
			if p == "" || r.re[p] != nil {
				continue
			}
			re, err := regexp.Compile(p)
			if err != nil {
				return fmt.Errorf("pattern %q: %w", p, err)
			}
			r.re[p] = re
		}
	}
	switch r.Kind {
	case "max_lines":
		if r.N <= 0 {
			return fmt.Errorf("max_lines needs n")
		}
	case "alert":
		if r.Expect == nil {
			return fmt.Errorf("alert needs expect")
		}
	case "protocol_lines":
		if r.Prefix == "" || len(r.MustCover) == 0 {
			return fmt.Errorf("protocol_lines needs a prefix and must_cover")
		}
	case "time_near":
		if !strings.HasPrefix(r.At, "{{t0") || !strings.HasSuffix(r.At, "}}") {
			return fmt.Errorf("time_near's at is a {{t0…}} placeholder")
		}
	case "card":
		if op.ExcludedAfter == "" || op.LimitedAfter == "" || op.Threshold == "" || op.MinSamples == "" || op.Pair == "" {
			return fmt.Errorf("card needs its option_parse")
		}
	}
	return nil
}

func (r *Rule) rx(p string) *regexp.Regexp { return r.re[p] }
