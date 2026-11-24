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
//
// Step by step (Owner 2026-11-24: first a framework that can hold good laws,
// in the product; then the laws put into it with how each is enforced and
// judged, not enforced yet; then enforcement law by law): the framework's
// checks are notes on the card the Owner reads when he approves it, not
// refusals. Only three things refuse a card, the register's integrity: a
// law changes only by a card (law_amend itself), its text before matches
// the register (CheckLawCard), and nothing loosens a fundamental law (a
// conflict with a level 1 law in force). How hard each rule is enforced
// follows the project's stage, decided law by law.

// The levels.
const (
	LevelDao = iota
	LevelFundamental
	LevelBasic
	LevelDomain
	LevelDetail
)

// Whether a law is enforced.
const (
	EnforceOff = "未执行"
	EnforceOn  = "执行中"
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
	Level        *int     `json:"level,omitempty"`
	Scope        string   `json:"scope,omitempty"`
	Subject      string   `json:"subject,omitempty"`
	Modality     string   `json:"modality,omitempty"`
	Condition    string   `json:"condition,omitempty"`
	Parent       string   `json:"parent,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
	Supersedes   []string `json:"supersedes,omitempty"`
	DistinctFrom string   `json:"distinct_from,omitempty"`
	Executor     string   `json:"executor,omitempty"`
	Enforcement  string   `json:"enforcement,omitempty"` // the check that enforces it, or 自觉
	Evidence     string   `json:"evidence,omitempty"`
	ExpiresOn    string   `json:"expires_on,omitempty"` // a stage law's last day, YYYY-MM-DD
	// Enforce is whether the law is being enforced: 未执行 (the default:
	// enacted, how it is enforced and judged is set, nothing is enforced yet)
	// or 执行中 (it is in the plan, the audit line measures it, its checks
	// run). It turns on one law at a time, by a card (Owner 2026-11-24:
	// frame first, then the laws, then enforcement law by law).
	Enforce string     `json:"enforce,omitempty"`
	Review  *LawReview `json:"review,omitempty"`
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
		Enforcement: s("enforcement"), Evidence: s("evidence"), ExpiresOn: s("expires_on"), Enforce: s("enforce")}
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

// FrameNotes says what the framework finds about a new version of law self
// (empty for an add): what its level still lacks, a parent not above it, a
// dependency or superseded law that is not there, a duplicate or a conflict
// on its scope and subject. The notes go on the card; they refuse nothing.
// The error is the one thing that refuses: a malformed frame (a level, a
// modality, an enforcement state or a date out of range) or a conflict with
// a fundamental law in force.
func FrameNotes(ctx context.Context, reg LawLookup, self string, f *LawFrame, standards int) ([]string, error) {
	if f == nil || f.Level == nil {
		return []string{"没有框架字段（层级等）：批了也只算素材，不是现行法"}, nil
	}
	lv := *f.Level
	if lv < LevelDao || lv > LevelDetail {
		return nil, fmt.Errorf("law_amend: level %d is not 0 to 4", lv)
	}
	if f.Modality != "" && !slices.Contains(lawModalities, f.Modality) {
		return nil, fmt.Errorf("law_amend: modality %q is not one of %v", f.Modality, lawModalities)
	}
	if f.Enforce != "" && f.Enforce != EnforceOff && f.Enforce != EnforceOn {
		return nil, fmt.Errorf("law_amend: enforce %q is not %s or %s", f.Enforce, EnforceOff, EnforceOn)
	}
	if f.ExpiresOn != "" {
		if _, err := time.Parse("2006-01-02", f.ExpiresOn); err != nil {
			return nil, fmt.Errorf("law_amend: expires_on %q is not YYYY-MM-DD", f.ExpiresOn)
		}
	}
	var notes []string
	var miss []string
	need := func(ok bool, name string) {
		if !ok {
			miss = append(miss, name)
		}
	}
	if lv >= LevelFundamental {
		need(f.Scope != "", "范围")
		need(f.Subject != "", "主题")
		need(f.Modality != "", "规定方式")
		need(f.Executor != "", "执行者")
	}
	if lv >= LevelFundamental && lv <= LevelDomain {
		need(f.Condition != "", "适用条件")
		need(f.Enforcement != "", "强制方式")
		need(f.Evidence != "", "证据")
		need(standards > 0, "标准")
		need(f.Review != nil && f.Review.By != "" && f.Review.At != "" && f.Review.Verdict == "可量", "审计复核（可量）")
	}
	if lv >= LevelDomain {
		need(f.Parent != "", "上位法")
	}
	if len(miss) > 0 {
		notes = append(notes, fmt.Sprintf("第 %d 层还缺：%s", lv, strings.Join(miss, "、")))
	}
	if f.Scope != "" && !lawScope.MatchString(f.Scope) {
		notes = append(notes, fmt.Sprintf("范围「%s」不合格式", f.Scope))
	}
	if f.Subject != "" && !slices.Contains(LawSubjects, f.Subject) {
		notes = append(notes, fmt.Sprintf("主题「%s」不在主题表里", f.Subject))
	}
	if lv == LevelFundamental && f.Enforcement == "自觉" {
		notes = append(notes, "根本法应由机器强制，不能靠自觉")
	}
	if reg.All == nil {
		return append(notes, "名录列不出来，没查上位法、依存、冲突和重复"), nil
	}
	laws, err := reg.All(ctx)
	if err != nil {
		return append(notes, "名录读不到，没查上位法、依存、冲突和重复"), nil
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
		if p, ok := inForce(f.Parent); !ok {
			notes = append(notes, fmt.Sprintf("上位法第 %s 条不是现行法", f.Parent))
		} else if *p.Frame.Level >= lv {
			notes = append(notes, fmt.Sprintf("上位法第 %s 条在第 %d 层，不比它高", f.Parent, *p.Frame.Level))
		}
	}
	for _, d := range f.DependsOn {
		if _, ok := inForce(d); !ok {
			notes = append(notes, fmt.Sprintf("依存的第 %s 条不是现行法", d))
		}
	}
	for _, sn := range f.Supersedes {
		if l, ok := by[sn]; !ok || LawState(l, now) == StateRepealed {
			notes = append(notes, fmt.Sprintf("要取代的第 %s 条不在名录或已废止", sn))
		}
	}
	if f.Scope == "" || f.Subject == "" {
		return notes, nil
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
			if *o.Level == LevelFundamental && lv > LevelFundamental {
				return nil, fmt.Errorf("law_amend: conflicts with fundamental law %s (%s, %s: %s against %s); nothing loosens a fundamental law",
					l.Law, f.Scope, f.Subject, o.Modality, f.Modality)
			}
			notes = append(notes, fmt.Sprintf("和第 %s 条冲突（%s／%s：%s 对 %s），应在同一张票里改或废它", l.Law, f.Scope, f.Subject, o.Modality, f.Modality))
			continue
		}
		if o.Modality == f.Modality && f.DistinctFrom == "" {
			notes = append(notes, fmt.Sprintf("和第 %s 条重复（%s／%s／%s），应写取代或差别", l.Law, f.Scope, f.Subject, f.Modality))
		}
	}
	return notes, nil
}
