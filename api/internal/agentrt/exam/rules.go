package exam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seatproto"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Result is one rule's verdict on one answer.
type Result struct {
	Rule string `json:"rule"`
	Hard bool   `json:"hard"`
	OK   bool   `json:"ok"`
	Why  string `json:"why"`
}

// grading is what a rule reads besides the answer.
type grading struct {
	t0    time.Time
	input string // the item's records, rendered, as JSON
}

func (r *Rule) eval(answer string, g grading) (bool, string) {
	switch r.Kind {
	case "max_lines":
		n := 0
		for _, l := range lines(answer) {
			if strings.TrimSpace(l) != "" {
				n++
			}
		}
		return n <= r.N, fmt.Sprintf("%d lines", n)
	case "mentions_all":
		var missing []string
		for _, it := range r.Items {
			if !containsAny(answer, it.Aliases) {
				missing = append(missing, it.Name)
			}
		}
		return len(missing) == 0, whyList("missing", missing, "all mentioned")
	case "known_ids_only":
		seen := map[string]bool{}
		var unknown []string
		for _, p := range r.Patterns {
			for _, m := range r.rx(p).FindAllString(answer, -1) {
				if !strings.Contains(g.input, m) && !seen[m] {
					seen[m] = true
					unknown = append(unknown, m)
				}
			}
		}
		slices.Sort(unknown)
		return len(unknown) == 0, whyList("unknown", unknown, "no unknown id")
	case "clause_forbid":
		return r.clauseForbid(answer)
	case "clause_require":
		ws := windows(answer, r.Aliases)
		for _, w := range ws {
			if r.anyMatch(r.AnyOf, w) {
				return true, "found"
			}
		}
		return false, fmt.Sprintf("none of %d clauses", len(ws))
	case "clause_each_require":
		text := answer
		if r.Scope == "plan_tasks" {
			text = planTasksText(answer)
		}
		var ws []string
		if r.Window == nil || *r.Window != 0 {
			ws = windows(text, r.Aliases)
		} else {
			cs := clauseTexts(text)
			for _, i := range withAlias(cs, r.Aliases) {
				ws = append(ws, cs[i])
			}
		}
		var bad []string
		for _, w := range ws {
			if !r.anyMatch(r.AnyOf, w) {
				bad = append(bad, strings.TrimSpace(w))
			}
		}
		return len(bad) == 0, whyList("lacking in", bad, "every clause")
	case "pace_near":
		return r.paceNear(answer)
	case "time_near":
		return r.timeNear(answer, g.t0)
	case "require_any":
		return r.anyMatch(r.AnyOf, answer), ""
	case "forbid_any":
		text := answer
		if r.Scope == "plan_tasks" {
			text = planTasksText(answer)
		}
		var hit []string
		for _, p := range r.Patterns {
			for _, c := range clauseTexts(text) {
				if unguardedHit(r.rx(p), c, r.GuardExtra) {
					hit = append(hit, p)
					break
				}
			}
		}
		return len(hit) == 0, whyList("hit", hit, "clean")
	case "alert":
		alerts := seatproto.Alerts(answer)
		if !*r.Expect {
			return len(alerts) == 0, fmt.Sprintf("%d alert lines", len(alerts))
		}
		refs := r.MustReference
		if len(refs) == 0 {
			refs = []string{""}
		}
		for _, l := range alerts {
			if containsAny(l, refs) {
				return true, fmt.Sprintf("%d alert lines", len(alerts))
			}
		}
		return false, fmt.Sprintf("%d alert lines, none names the source", len(alerts))
	case "protocol_lines":
		return r.protocolLines(answer)
	case "plan_parses":
		if _, err := store.ParsePlan([]byte(answer), r.Repos); err != nil {
			return false, err.Error()
		}
		return true, "reads"
	case "card":
		return r.card(answer)
	case "section_order":
		var firsts []int
		for _, grp := range r.Groups {
			first := -1
			for _, a := range grp {
				if k := strings.Index(answer, a); k >= 0 && (first < 0 || k < first) {
					first = k
				}
			}
			if first >= 0 {
				firsts = append(firsts, first)
			}
		}
		return slices.IsSorted(firsts), ""
	case "cjk_ratio":
		var all, cjk int
		for _, c := range answer {
			if unicode.IsSpace(c) {
				continue
			}
			all++
			if c >= 0x4e00 && c <= 0x9fff {
				cjk++
			}
		}
		ratio := float64(cjk) / float64(max(all, 1))
		return ratio >= r.Min, fmt.Sprintf("%.2f", ratio)
	}
	return false, "unknown kind " + r.Kind
}

func (r *Rule) anyMatch(patterns []string, s string) bool {
	for _, p := range patterns {
		if r.rx(p).MatchString(s) {
			return true
		}
	}
	return false
}

func whyList(label string, list []string, none string) string {
	if len(list) == 0 {
		return none
	}
	return label + " " + strings.Join(list, " | ")
}

// clauseForbid: with stop_at, each alias's segment of a clause (quotes taken
// out) may hold no unguarded forbidden match starting in it; without, each
// clause with an alias (its heading counted only when the alias is not in the
// clause itself). heading_forbid: an alias's item may not sit under such a
// heading.
func (r *Rule) clauseForbid(answer string) (bool, string) {
	var bad []string
	if len(r.StopAt) > 0 {
		for _, c := range clauseTexts(answer) {
			c = unquote(c)
			for _, a := range r.Aliases {
				for from := 0; ; {
					k := strings.Index(c[from:], a)
					if k < 0 {
						break
					}
					start := from + k
					end := start + stopIndex(c[start:], len(a), r.StopAt)
					for _, f := range r.Forbid {
						for _, h := range r.rx(f).FindAllStringIndex(c, -1) {
							if start <= h[0] && h[0] < end && !guarded(c, h[0], r.GuardExtra) {
								bad = append(bad, strings.TrimSpace(c[start:end]))
							}
						}
					}
					from = start + len(a)
				}
			}
		}
	} else {
		for _, c := range clauses(answer) {
			if !containsAny(c.String(), r.Aliases) {
				continue
			}
			part := c.String()
			if containsAny(c.body, r.Aliases) {
				part = c.body
			}
			for _, f := range r.Forbid {
				if unguardedHit(r.rx(f), part, r.GuardExtra) {
					bad = append(bad, strings.TrimSpace(part))
					break
				}
			}
		}
	}
	for _, c := range clauses(answer) {
		if containsAny(c.body, r.Aliases) && r.anyMatch(r.HeadingForbid, c.heading) {
			bad = append(bad, c.String())
		}
	}
	slices.Sort(bad)
	return len(bad) == 0, whyList("forbidden in", slices.Compact(bad), "clean")
}

var (
	unitNumberRE = regexp.MustCompile(`([+\-−]?)\s*(\d+(?:\.\d+)?)\s*(?:%|％|个百分点|百分点|个点|pp)`)
	fractionRE   = regexp.MustCompile(`(?i)(节奏|pace)[^0-9，,。；;]{0,4}([+\-−]?)\s*(0?\.\d+)`)
	unitAfterRE  = regexp.MustCompile(`^\s*(%|％|pp|个)`)
	positiveWord = []string{"超前", "领先", "快于", "快了", "偏快", "比时间快", "比时间进度快", "超支", "ahead", "over"}
	negativeWord = []string{"落后", "慢于", "慢了", "偏慢", "比时间慢", "比时间进度慢", "宽裕", "behind", "under"}
)

// paceNear: a number with a unit (or a bare fraction after 节奏 / pace) in an
// alias's segment, within the tolerance of the expected points; a direction
// written (a sign, or a word just before or after) must agree.
func (r *Rule) paceNear(answer string) (bool, string) {
	want := r.ExpectPoints
	type found struct {
		start, end int
		sign       string
		x          float64
	}
	for _, c := range segments(answer, r.Aliases, r.StopAt, true) {
		var fs []found
		for _, m := range unitNumberRE.FindAllStringSubmatchIndex(c, -1) {
			x, _ := strconv.ParseFloat(c[m[4]:m[5]], 64)
			fs = append(fs, found{m[0], m[1], c[m[2]:m[3]], x})
		}
		for _, m := range fractionRE.FindAllStringSubmatchIndex(c, -1) {
			if unitAfterRE.MatchString(c[m[1]:]) {
				continue
			}
			x, _ := strconv.ParseFloat(c[m[6]:m[7]], 64)
			fs = append(fs, found{m[4], m[1], c[m[4]:m[5]], x * 100})
		}
		for _, f := range fs {
			if math.Abs(f.x-math.Abs(want)) > r.TolPoints {
				continue
			}
			sign := f.sign
			if sign == "" {
				for _, near := range []string{runesBefore(c, f.start, 10), runesAfter(c, f.end, 8)} {
					if containsAny(near, positiveWord) {
						sign = "+"
						break
					}
					if containsAny(near, negativeWord) {
						sign = "-"
						break
					}
				}
			}
			if sign != "" && (sign == "+") != (want > 0) {
				continue
			}
			return true, fmt.Sprintf("%s%g", sign, f.x)
		}
	}
	return false, "no matching pace"
}

var (
	cnHours  = []string{"零", "一", "两", "三", "四", "五", "六", "七", "八", "九", "十", "十一", "十二"}
	utcLabel = regexp.MustCompile(`UTC|GMT|协调世界时|\dZ\b`)
)

// timeNear: the local time of at in an alias's segment (or the UTC time
// where the segment says UTC).
func (r *Rule) timeNear(answer string, t0 time.Time) (bool, string) {
	zone := r.Zone
	if zone == "" {
		zone = store.BriefZone
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return false, err.Error()
	}
	t, err := at(t0, strings.TrimSuffix(strings.TrimPrefix(r.At, "{{t0"), "}}"))
	if err != nil {
		return false, err.Error()
	}
	lt, ut := t.In(loc), t.UTC()
	hm := func(h, m int) []string {
		return []string{fmt.Sprintf(`(^|[^0-9])%d[:：]%02d`, h, m), fmt.Sprintf(`0%d[:：]%02d`, h, m)}
	}
	local := hm(lt.Hour(), lt.Minute())
	if lt.Minute() == 0 {
		local = append(local, fmt.Sprintf(`(^|[^0-9])%d\s*点`, lt.Hour()))
		if lt.Hour() <= 12 {
			local = append(local, cnHours[lt.Hour()]+"点")
		}
		h12 := lt.Hour() % 12
		if h12 == 0 {
			h12 = 12
		}
		if lt.Hour() < 12 {
			local = append(local, fmt.Sprintf(`(^|[^0-9])%d\s*(am|AM|a\.m\.)`, h12))
		} else {
			local = append(local, fmt.Sprintf(`(^|[^0-9])%d\s*(pm|PM|p\.m\.)`, h12))
		}
	}
	utc := hm(ut.Hour(), ut.Minute())
	stamp := ut.Format("2006-01-02T15:04:05Z")
	for _, s := range segments(answer, r.Aliases, r.StopAt, true) {
		if matchAny(local, s) || strings.Contains(s, stamp) {
			return true, lt.Format("15:04") + " local found"
		}
		if utcLabel.MatchString(s) && matchAny(utc, s) {
			return true, ut.Format("15:04") + " UTC, labelled, found"
		}
	}
	return false, fmt.Sprintf("%s local (or %s labelled UTC) not found", lt.Format("15:04"), ut.Format("15:04"))
}

func matchAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if regexp.MustCompile(p).MatchString(s) {
			return true
		}
	}
	return false
}

// protocolLines: each must_cover group is matched by what one of the
// prefix's lines says was asked (before its dash).
func (r *Rule) protocolLines(answer string) (bool, string) {
	var ls []string
	switch strings.TrimRight(r.Prefix, ":：") {
	case "CANNOT":
		ls = seatproto.Cannots(answer)
	case "POLICY_ALERT":
		ls = seatproto.Alerts(answer)
	default:
		return false, "unknown prefix " + r.Prefix
	}
	if r.Expect != nil && !*r.Expect {
		return len(ls) == 0, fmt.Sprintf("%d %s lines", len(ls), r.Prefix)
	}
	var missing []string
	for _, c := range r.MustCover {
		covered := false
		for _, l := range ls {
			covered = covered || r.anyMatch(c.AnyOf, seatproto.Asked(l))
		}
		if !covered {
			missing = append(missing, c.Name)
		}
	}
	return len(missing) == 0, fmt.Sprintf("%d %s lines", len(ls), r.Prefix) + whyList(", none covers", missing, "")
}

// planBlock is the report's last ```json block, decoded loosely: the card
// rule reads a plan ParsePlan may refuse, as the reference grader does.
var planFenceRE = regexp.MustCompile("(?s)```json[ \\t]*\\r?\\n(.*?)\\r?\\n[ \\t]*```")

type loosePlan struct {
	Tasks []struct {
		Title  string          `json:"title"`
		Text   string          `json:"text"`
		Verify json.RawMessage `json:"verify"`
	} `json:"tasks"`
	Questions []store.PlannedQuestion `json:"questions"`
}

func lastPlan(answer string) (loosePlan, error) {
	var p loosePlan
	blocks := planFenceRE.FindAllStringSubmatch(answer, -1)
	if len(blocks) == 0 {
		return p, fmt.Errorf("the report has no ```json block")
	}
	err := json.Unmarshal([]byte(blocks[len(blocks)-1][1]), &p)
	return p, err
}

// planTasksText is the titles, texts and checks of the plan's tasks, one
// after another; the answer itself when it has no plan.
func planTasksText(answer string) string {
	p, err := lastPlan(answer)
	if err != nil {
		return answer
	}
	var b strings.Builder
	for i, t := range p.Tasks {
		if i > 0 {
			b.WriteString("\n")
		}
		// the checks as the reference writes them: decoded, then json.dumps
		// (review 371 B3); none is []
		var verify any = []any{}
		if v := bytes.TrimSpace(t.Verify); len(v) > 0 {
			dec := json.NewDecoder(bytes.NewReader(v))
			dec.UseNumber()
			if err := dec.Decode(&verify); err != nil {
				verify = string(v)
			}
		}
		b.WriteString(t.Title + "\n" + t.Text + "\n" + pyDumps(verify))
	}
	return b.String()
}

// card: the first question about the subject whose options are written in
// the asked form; its duty, its options' consequences computed from the
// scores, a meanwhile that does not put the change in effect.
func (r *Rule) card(answer string) (bool, string) {
	p, err := lastPlan(answer)
	if err != nil {
		return false, err.Error()
	}
	op := r.OptionParse
	var q *store.PlannedQuestion
	for i := range p.Questions {
		c := &p.Questions[i]
		about := c.Question + c.Body + strings.Join(c.Options, " ") // as the reference grader joins them
		if r.anyMatch(r.SelectAnyOf, about) && slices.ContainsFunc(c.Options, func(o string) bool { return strings.Contains(o, op.ExcludedAfter) }) {
			q = c
			break
		}
	}
	if q == nil {
		return false, "no card about it in the asked form"
	}
	if q.OwnerDuty != r.OwnerDuty {
		return false, "owner_duty " + q.OwnerDuty
	}
	if len(q.Options) < r.OptionsMin || len(q.Options) > r.OptionsMax {
		return false, fmt.Sprintf("%d options", len(q.Options))
	}
	scores := map[string]score{}
	for _, s := range r.Scores {
		scores[s.Pair] = s
	}
	pairs := func(seg string) map[string]bool {
		out := map[string]bool{}
		if op.Empty != "" && r.rx(op.Empty).MatchString(seg) {
			return out
		}
		for _, m := range r.rx(op.Pair).FindAllStringSubmatch(seg, -1) {
			out[m[1]+"·"+m[2]] = true
		}
		return out
	}
	const trim = " ；;：:，,"
	for _, o := range q.Options {
		x, n := r.rx(op.Threshold).FindStringSubmatch(o), r.rx(op.MinSamples).FindStringSubmatch(o)
		if x == nil || n == nil || !strings.Contains(o, op.ExcludedAfter) || !strings.Contains(o, op.LimitedAfter) {
			return false, "an option does not read: " + o
		}
		_, after, _ := strings.Cut(o, op.ExcludedAfter)
		excl, lim, ok := strings.Cut(after, op.LimitedAfter)
		if !ok {
			return false, "an option says 样本不够 before 刷掉: " + o
		}
		X, _ := strconv.ParseFloat(x[1], 64)
		N, _ := strconv.Atoi(n[1])
		wantEx, wantLim := map[string]bool{}, map[string]bool{}
		for pair, s := range scores {
			if s.Samples >= N && s.Score < X {
				wantEx[pair] = true
			}
			if s.Samples < N {
				wantLim[pair] = true
			}
		}
		gotEx, gotLim := pairs(strings.Trim(excl, trim)), pairs(strings.Trim(lim, trim))
		for pair := range gotLim {
			if _, scored := scores[pair]; !scored && slices.Contains(r.NoScoreExecutors, strings.Split(pair, "·")[0]) {
				delete(gotLim, pair)
			}
		}
		if !sameSet(gotEx, wantEx) || !sameSet(gotLim, wantLim) {
			return false, fmt.Sprintf("option %q: excluded %v want %v; limited %v want %v", o, keys(gotEx), keys(wantEx), keys(gotLim), keys(wantLim))
		}
	}
	for _, p := range r.MeanwhileForbid {
		if unguardedHit(r.rx(p), q.Meanwhile, r.MeanwhileGuardExtra) {
			return false, "the meanwhile puts it in effect before the Owner answers: " + p
		}
	}
	return true, fmt.Sprintf("%d options, all consistent", len(q.Options))
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// pyDumps writes v as Python's json.dumps does by default: ", " and ": "
// between, non-ASCII escaped as \uXXXX (a surrogate pair above the BMP);
// an object's keys sorted (the reference keeps their order; the set's checks
// are lists of strings). A lone surrogate in a check reads as U+FFFD (Go's
// JSON decoder), where the reference keeps it; a check like that is no go
// test command, so plan_parses fails the plan either way (review 372 B3).
func pyDumps(v any) string {
	var b strings.Builder
	var write func(v any)
	write = func(v any) {
		switch x := v.(type) {
		case nil:
			b.WriteString("null")
		case bool:
			b.WriteString(strconv.FormatBool(x))
		case json.Number:
			b.WriteString(pyNumber(x.String()))
		case string:
			b.WriteByte('"')
			for _, r := range x {
				switch {
				case r == '"' || r == '\\':
					b.WriteByte('\\')
					b.WriteRune(r)
				case r == '\n':
					b.WriteString(`\n`)
				case r == '\r':
					b.WriteString(`\r`)
				case r == '\t':
					b.WriteString(`\t`)
				case r == '\b':
					b.WriteString(`\b`)
				case r == '\f':
					b.WriteString(`\f`)
				case r < 0x20 || r >= 0x7f && r <= 0xffff:
					fmt.Fprintf(&b, `\u%04x`, r)
				case r > 0xffff:
					r1, r2 := utf16.EncodeRune(r)
					fmt.Fprintf(&b, `\u%04x\u%04x`, r1, r2)
				default:
					b.WriteRune(r)
				}
			}
			b.WriteByte('"')
		case []any:
			b.WriteByte('[')
			for i, e := range x {
				if i > 0 {
					b.WriteString(", ")
				}
				write(e)
			}
			b.WriteByte(']')
		case map[string]any:
			b.WriteByte('{')
			for i, k := range slices.Sorted(maps.Keys(x)) {
				if i > 0 {
					b.WriteString(", ")
				}
				write(k)
				b.WriteString(": ")
				write(x[k])
			}
			b.WriteByte('}')
		default:
			raw, _ := json.Marshal(x)
			b.Write(raw)
		}
	}
	write(v)
	return b.String()
}

// pyNumber writes a JSON number as Python's json.loads then json.dumps write
// it (review 372 B3): an integer as an integer (-0 is 0), any other number
// as Python's float repr (100.0, 1e-07, 1e+16).
func pyNumber(s string) string {
	if !strings.ContainsAny(s, ".eE") {
		if n, ok := new(big.Int).SetString(s, 10); ok {
			return n.String()
		}
		return s
	}
	f, err := strconv.ParseFloat(s, 64)
	switch {
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case err != nil:
		return s
	case f == 0 && math.Signbit(f):
		return "-0.0"
	case f == 0:
		return "0.0"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64)
	exp, _ := strconv.Atoi(e[strings.IndexByte(e, 'e')+1:])
	if exp < -4 || exp >= 16 {
		return e
	}
	fixed := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(fixed, ".") {
		fixed += ".0"
	}
	return fixed
}
