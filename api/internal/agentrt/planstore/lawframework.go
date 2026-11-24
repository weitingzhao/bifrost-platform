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
	// 方法: how to judge and in what order to act, the laws every other law
	// leans on (2026-11-24: first principles and the five steps, Owner
	// 「五步法按照马斯克的原话写两段」).
	"方法",
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
	Level      *int     `json:"level,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Subject    string   `json:"subject,omitempty"`
	Modality   string   `json:"modality,omitempty"`
	Condition  string   `json:"condition,omitempty"`
	Parent     string   `json:"parent,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
	Supersedes []string `json:"supersedes,omitempty"`
	// Absorbs names the material (素材) this law distils: the numbers of the
	// transcribed rows it takes in (Owner 2026-11-24, step two: distil the
	// material law by law, then confirm once that the new laws cover all of
	// it before the material is repealed).
	Absorbs      []string `json:"absorbs,omitempty"`
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
		DependsOn: list("depends_on"), Supersedes: list("supersedes"), Absorbs: list("absorbs"), DistinctFrom: s("distinct_from"), Executor: s("executor"),
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
	// A repealed law cannot be cited: not as a parent, a dependency, a law
	// superseded or material absorbed (Owner 2026-11-24: a repealed law must
	// not linger as poison). This refuses the card.
	cited := map[string][]string{"parent": {f.Parent}, "depends_on": f.DependsOn, "supersedes": f.Supersedes, "absorbs": f.Absorbs}
	for field, ns := range cited {
		for _, n := range ns {
			if l, ok := by[n]; ok && n != "" && LawState(l, now) == StateRepealed {
				return nil, fmt.Errorf("law_amend: law %s is repealed and cannot be cited (%s)", n, field)
			}
		}
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
	for _, a := range f.Absorbs {
		if l, ok := by[a]; !ok || LawState(l, now) != StateMaterial {
			notes = append(notes, fmt.Sprintf("吸收的第 %s 条不是素材", a))
		}
	}
	if lv >= LevelFundamental && len(f.Absorbs) == 0 {
		notes = append(notes, "没写吸收了哪些素材（absorbs）")
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

// FrameworkLevel is one level as the framework defines it.
type FrameworkLevel struct {
	Level    int      `json:"level"`
	Name     string   `json:"name"`
	Meaning  string   `json:"meaning"`
	Required []string `json:"required"` // field names a law at this level carries
	InForce  int      `json:"in_force"` // laws in force at this level now
}

// FrameworkField is one part of a law.
type FrameworkField struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Meaning string `json:"meaning"`
}

// Framework is the framework of laws as the system holds it (法的框架 v1),
// read with the register so a page shows the frame even when no law is in
// it yet (Owner 2026-11-24: step one is a framework, empty, seen on Ops).
type Framework struct {
	Version    string           `json:"version"`
	Source     string           `json:"source"`
	Levels     []FrameworkLevel `json:"levels"`
	Fields     []FrameworkField `json:"fields"`
	Subjects   []string         `json:"subjects"`
	ScopeForms []string         `json:"scope_forms"`
	Modalities []string         `json:"modalities"`
	States     map[string]int   `json:"states"` // how many laws are in each state now
	// Coverage is how much of the material the laws in force absorb.
	Coverage  FrameworkCoverage `json:"coverage"`
	Enforce   []string          `json:"enforce"`
	Between   []string          `json:"between"`   // the rules between laws (notes on a card)
	Integrity []string          `json:"integrity"` // what refuses a card
	StageNote string            `json:"stage_note"`
}

// TheFramework is the framework with the register's counts.
func TheFramework(laws []Law, now time.Time) Framework {
	base := []string{"text", "source"}
	rule := []string{"scope", "subject", "modality", "executor"}
	full := []string{"condition", "enforcement", "evidence", "standards", "review"}
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	fw := Framework{
		Version: "v1", Source: "法的框架 v1, approved by the Owner; https://claude.ai/artifact/TR91x7uM1feLXMPrJ639T6",
		Levels: []FrameworkLevel{
			{Level: LevelDao, Name: "道", Meaning: "信念和方向；不量、不直接执行，是下面各层的依据；由 Owner 亲自定", Required: base},
			{Level: LevelFundamental, Name: "根本法", Meaning: "收不回的硬边界（六类）；机器必须拦，任何下位法不能放松", Required: cat(base, rule, full)},
			{Level: LevelBasic, Name: "基本法", Meaning: "全系统通用的规矩", Required: cat(base, rule, full)},
			{Level: LevelDomain, Name: "领域法", Meaning: "只适用于某个领域或某条线；不得和上位法抵触", Required: cat(base, rule, full, []string{"parent"})},
			{Level: LevelDetail, Name: "细则与判例", Meaning: "落实上位法的具体做法和一次性决定；领域 owner 在授权内自定、登记、可撤", Required: cat(base, rule, []string{"parent"})},
		},
		Fields: []FrameworkField{
			{"text", "原文", "一句话说清规矩"},
			{"scope", "适用范围", "全系统、某个域、某个领域、某个环境或某类动作"},
			{"subject", "主题", "从主题表里选，用来找重复和冲突"},
			{"modality", "规定方式", "必须、禁止或允许"},
			{"condition", "适用条件", "在什么情况下适用（假定）"},
			{"parent", "上位法", "它落实或细化的是哪一条"},
			{"depends_on", "依存", "要先有哪几条成立"},
			{"supersedes", "取代", "它取代哪几条；被取代的随之废止并移出名录"},
			{"absorbs", "吸收的素材", "它从哪几条素材提炼而来；素材全被吸收后可一次废止"},
			{"executor", "执行者", "哪条线负责执行"},
			{"enforcement", "强制方式", "哪道机器检查强制，或写明靠自觉"},
			{"evidence", "证据", "从哪个读口取证，是否系统存档"},
			{"standards", "标准和量法", "审计按什么判达到、没到、没量（后果）"},
			{"review", "审计复核", "建卡前审计看标准能不能量：谁、何时、可量或量不了"},
			{"enforce", "执法开关", "未执行或执行中；执行中才进计划、被审计量"},
			{"expires_on", "有效期", "阶段法的最后一天"},
			{"source", "出处", "谁在哪天说的、吸收了哪些素材、批准卡号"},
		},
		Subjects:   LawSubjects,
		ScopeForms: []string{"全系统", "域:Trade|Research|Ops", "领域:<领域 id>", "环境:PROD|STG|DEV", "动作:<一类动作>"},
		Modalities: lawModalities,
		States:     map[string]int{StateInForce: 0, StateMaterial: 0},
		Enforce:    []string{EnforceOff, EnforceOn},
		Between: []string{
			"上位优先：下位法和上位法抵触的那一处不执行",
			"同层：范围窄的优先；同样范围，新批的优先",
			"冲突：同范围同主题规定方式相反的，应在同一张票里改或废对方",
			"重复：同范围同主题同方式的，应写明取代或差别",
			"依存：依存的法必须现行；被依存的要废止时一并处理",
			"废止即移出名录：不再被读、引用、执行或量，只在账本里留底供追溯；引用废止的法的卡被拒",
		},
		Integrity: []string{
			"法只经 Owner 批准的修法票增、改、废",
			"修法票的改前原文必须和名录对得上",
			"不能放松根本法（与现行第 1 层法冲突的卡被拒）",
			"不能引用已废止的法（作上位法、依存、取代或吸收）",
		},
		StageNote: "现阶段框架的发现只在卡面提示、不拦；执法一条一条开，力度随项目阶段定",
	}
	absorbed := AbsorbedBy(laws, now)
	fw.Coverage.Uncovered = []string{}
	for _, l := range laws {
		st := LawState(l, now)
		if st == StateRepealed {
			continue
		}
		fw.States[st]++
		if st == StateInForce && l.Frame != nil && l.Frame.Level != nil {
			fw.Levels[*l.Frame.Level].InForce++
		}
		if st == StateMaterial {
			fw.Coverage.Material++
			if len(absorbed[l.Law]) > 0 {
				fw.Coverage.Covered++
			} else {
				fw.Coverage.Uncovered = append(fw.Coverage.Uncovered, l.Law)
			}
		}
	}
	return fw
}

// AbsorbedBy maps each material row to the laws in force that absorb it.
func AbsorbedBy(laws []Law, now time.Time) map[string][]string {
	out := map[string][]string{}
	for _, l := range laws {
		if LawState(l, now) != StateInForce || l.Frame == nil {
			continue
		}
		for _, a := range l.Frame.Absorbs {
			out[a] = append(out[a], l.Law)
		}
	}
	return out
}

// FrameworkCoverage is how much of the material the laws in force absorb:
// the material, how many of it at least one law absorbs, and which none does.
type FrameworkCoverage struct {
	Material  int      `json:"material"`
	Covered   int      `json:"covered"`
	Uncovered []string `json:"uncovered"`
}
