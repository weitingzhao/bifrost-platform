package planstore

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The framework of laws (法的框架 v1, Owner 2026-11-24: 「挺好的，就按这个来」;
// https://claude.ai/artifact/TR91x7uM1feLXMPrJ639T6). Written as code so it
// does not drift: a law is in force only when it was approved by a law_amend
// card and carries what its level requires; a card that would bring in a
// conflict, an unexplained duplicate, a missing dependency or a parent that
// is not above it is refused when it is created.
//
// Five levels, the higher first: 0 道 (beliefs, not measured), 1 根本法 (the
// six hard boundaries, enforced by a machine), 2 基本法 (system-wide), 3
// 领域法 (one domain or line), 4 细则与判例 (the details a domain owner sets
// within a higher law). Three states: 现行 (in force), 素材 (raw material:
// what was said, kept with its source, not executed: every transcribed row
// starts here), 废止.

// The levels.
const (
	LevelDao = iota
	LevelFundamental
	LevelBasic
	LevelDomain
	LevelDetail
)

// The states of a law.
const (
	StateInForce  = "现行"
	StateMaterial = "素材"
	StateRepealed = "废止"
)

// The modalities a rule takes.
var lawModalities = []string{"必须", "禁止", "允许"}

// LawSubjects is the controlled list of subjects (a detail registered with
// the framework; extended by a law_amend card). A law names one, so that
// laws on the same subject and scope can be compared.
var LawSubjects = []string{
	"审批", "发布", "合入", "数据", "合约", "归属", "算力", "观测", "计划", "审计",
	"立法", "沟通", "安全", "交易", "工坊", "结构", "评估", "执行口",
}

// A scope is the whole system, a domain of the system, one domain line, one
// environment or one kind of action.
var lawScope = regexp.MustCompile(`^(全系统|域:(Trade|Research|Ops)|领域:[a-z0-9-]+|环境:(PROD|STG|DEV)|动作:\S.{0,60})$`)

// LawReview is the audit line's review of a card's standards before it is
// created (framework: 「建卡前审计复核量法」).
type LawReview struct {
	By      string `json:"by"`
	At      string `json:"at"`
	Verdict string `json:"verdict"` // 可量
	Note    string `json:"note,omitempty"`
}

// LawFrame is what the framework asks of a law beyond its text and standards.
type LawFrame struct {
	Level        *int       `json:"level,omitempty"`
	Scope        string     `json:"scope,omitempty"`
	Subject      string     `json:"subject,omitempty"`
	Modality     string     `json:"modality,omitempty"`
	Condition    string     `json:"condition,omitempty"`
	Parent       string     `json:"parent,omitempty"`
	DependsOn    []string   `json:"depends_on,omitempty"`
	Supersedes   []string   `json:"supersedes,omitempty"`
	DistinctFrom string     `json:"distinct_from,omitempty"`
	Executor     string     `json:"executor,omitempty"`
	Enforcement  string     `json:"enforcement,omitempty"` // the check that enforces it, or 自觉
	Evidence     string     `json:"evidence,omitempty"`
	ExpiresOn    string     `json:"expires_on,omitempty"` // a stage law's last day, YYYY-MM-DD
	Review       *LawReview `json:"review,omitempty"`
}

// readLawFrame reads the card's frame param; nil when the card has none.
func readLawFrame(raw any) (*LawFrame, error) {
	m, ok := raw.(map[string]any)
	if raw == nil {
		return nil, nil
	}
	if !ok {
		return nil, errors.New("law_amend: frame must be an object")
	}
	s := func(k string) string { v, _ := m[k].(string); return strings.TrimSpace(v) }
	list := func(k string) []string {
		var out []string
		raw, _ := m[k].([]any)
		for _, r := range raw {
			if v, ok := r.(string); ok && strings.TrimSpace(v) != "" {
				out = append(out, strings.TrimSpace(v))
			}
		}
		return out
	}
	f := &LawFrame{Scope: s("scope"), Subject: s("subject"), Modality: s("modality"), Condition: s("condition"), Parent: s("parent"),
		DependsOn: list("depends_on"), Supersedes: list("supersedes"), DistinctFrom: s("distinct_from"), Executor: s("executor"),
		Enforcement: s("enforcement"), Evidence: s("evidence"), ExpiresOn: s("expires_on")}
	switch v := m["level"].(type) {
	case float64:
		n := int(v)
		f.Level = &n
	case int:
		f.Level = &v
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			f.Level = &n
		}
	}
	if r, ok := m["review"].(map[string]any); ok {
		g := func(k string) string { v, _ := r[k].(string); return strings.TrimSpace(v) }
		f.Review = &LawReview{By: g("by"), At: g("at"), Verdict: g("verdict"), Note: g("note")}
	}
	return f, nil
}

// checkFrameShape checks what a frame must carry at its level, before any
// other law is read.
func checkFrameShape(f *LawFrame, standards int) error {
	if f == nil || f.Level == nil {
		return errors.New("law_amend: the framework asks every new version of a law for a frame with its level (0 道, 1 根本法, 2 基本法, 3 领域法, 4 细则)")
	}
	lv := *f.Level
	if lv < LevelDao || lv > LevelDetail {
		return fmt.Errorf("law_amend: level %d is not 0 to 4", lv)
	}
	var miss []string
	need := func(ok bool, name string) {
		if !ok {
			miss = append(miss, name)
		}
	}
	if lv >= LevelFundamental {
		need(f.Scope != "", "scope")
		need(f.Subject != "", "subject")
		need(f.Modality != "", "modality")
		need(f.Executor != "", "executor")
	}
	if lv >= LevelFundamental && lv <= LevelDomain {
		need(f.Condition != "", "condition")
		need(f.Enforcement != "", "enforcement")
		need(f.Evidence != "", "evidence")
		need(standards > 0, "standards")
		need(f.Review != nil && f.Review.By != "" && f.Review.At != "" && f.Review.Verdict == "可量", "review (the audit line's review, verdict 可量)")
	}
	if lv >= LevelDomain {
		need(f.Parent != "", "parent")
	}
	if len(miss) > 0 {
		return fmt.Errorf("law_amend: a level %d law needs %s", lv, strings.Join(miss, ", "))
	}
	if f.Scope != "" && !lawScope.MatchString(f.Scope) {
		return fmt.Errorf("law_amend: scope %q is not 全系统, 域:Trade|Research|Ops, 领域:<id>, 环境:PROD|STG|DEV or 动作:<kind>", f.Scope)
	}
	if f.Subject != "" && !slices.Contains(LawSubjects, f.Subject) {
		return fmt.Errorf("law_amend: subject %q is not in the subject list %v", f.Subject, LawSubjects)
	}
	if f.Modality != "" && !slices.Contains(lawModalities, f.Modality) {
		return fmt.Errorf("law_amend: modality %q is not one of %v", f.Modality, lawModalities)
	}
	if lv == LevelFundamental && f.Enforcement == "自觉" {
		return errors.New("law_amend: a fundamental law (level 1) is enforced by a machine, not 自觉")
	}
	if f.ExpiresOn != "" {
		if _, err := time.Parse("2006-01-02", f.ExpiresOn); err != nil {
			return fmt.Errorf("law_amend: expires_on %q is not YYYY-MM-DD", f.ExpiresOn)
		}
	}
	return nil
}

// LawState is a law's state in the framework: 废止 when repealed or a stage
// law past its last day; 素材 when it only came from the file or carries no
// level; 现行 otherwise.
func LawState(l Law, today time.Time) string {
	if l.Status == RepealedStatus {
		return StateRepealed
	}
	if l.Frame != nil && l.Frame.ExpiresOn != "" {
		if end, err := time.Parse("2006-01-02", l.Frame.ExpiresOn); err == nil && today.After(end.Add(24*time.Hour)) {
			return StateRepealed
		}
	}
	if l.Op == "transcribed" || l.Frame == nil || l.Frame.Level == nil {
		return StateMaterial
	}
	return StateInForce
}

// checkFrameAgainst checks a new version of law self (empty for an add)
// against the register as it stands: its parent, what it depends on, what it
// supersedes, and conflicts and duplicates on its scope and subject.
func checkFrameAgainst(ctx context.Context, reg LawLookup, self string, f *LawFrame) error {
	if reg.All == nil {
		return errors.New("law_amend: the register of laws cannot be listed here; no card without it")
	}
	laws, err := reg.All(ctx)
	if err != nil {
		return fmt.Errorf("law_amend: reading the register: %w", err)
	}
	now := time.Now()
	by := map[string]Law{}
	for _, l := range laws {
		by[l.Law] = l
	}
	inForce := func(n string) (Law, bool) {
		l, ok := by[n]
		return l, ok && LawState(l, now) == StateInForce
	}
	if f.Parent != "" {
		p, ok := inForce(f.Parent)
		if !ok {
			return fmt.Errorf("law_amend: parent law %s is not a law in force", f.Parent)
		}
		if *p.Frame.Level >= *f.Level {
			return fmt.Errorf("law_amend: parent law %s is level %d, not above level %d", f.Parent, *p.Frame.Level, *f.Level)
		}
	}
	for _, d := range f.DependsOn {
		if _, ok := inForce(d); !ok {
			return fmt.Errorf("law_amend: law %s it depends on is not a law in force", d)
		}
	}
	for _, s := range f.Supersedes {
		l, ok := by[s]
		if !ok || LawState(l, now) == StateRepealed {
			return fmt.Errorf("law_amend: law %s it supersedes is not in the register or already repealed", s)
		}
	}
	if f.Scope == "" || f.Subject == "" {
		return nil
	}
	for _, l := range laws {
		if l.Law == self || slices.Contains(f.Supersedes, l.Law) || LawState(l, now) != StateInForce {
			continue
		}
		o := l.Frame
		if o.Scope != f.Scope || o.Subject != f.Subject {
			continue
		}
		if o.Modality != f.Modality && (o.Modality == "禁止" || f.Modality == "禁止") {
			return fmt.Errorf("law_amend: conflicts with law %s (%s, %s: %s against %s); amend or repeal it on this card (supersedes)",
				l.Law, f.Scope, f.Subject, o.Modality, f.Modality)
		}
		if o.Modality == f.Modality && f.DistinctFrom == "" {
			return fmt.Errorf("law_amend: law %s already rules %s on %s, %s; supersede it or say how this one differs (distinct_from)",
				l.Law, f.Modality, f.Subject, f.Scope)
		}
	}
	return nil
}
