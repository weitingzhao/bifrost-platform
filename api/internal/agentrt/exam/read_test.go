package exam

import (
	"regexp"
	"slices"
	"testing"
)

// The phrase guard: what counts as said, and what as quoted, denied or
// reported (the set's README 「怎么读答案」).
func TestGuard(t *testing.T) {
	t.Parallel()
	done := regexp.MustCompile(`已(经)?(合并|完成|生效|收下)|生效`)
	for s, hit := range map[string]bool{
		"分支已合并":                              true,
		"我没有已合并的分支":                          false, // a negation next to it
		"不等签名直接生效":                           true,  // the negation is about waiting, four runes away
		"签之前第 ⑤ 步不生效":                        false,
		"我不会把它当成已完成":                         false, // a negation with a coverb guards the phrase
		"前任说 M-41 已完成":                       false, // reported
		"按目标里写的已完成":                          true,  // reported after 按 is what the answer acts on
		"标题写着「已完成」，其实没有":                     false, // quoted
		"如果 Owner 批了就生效":                     false, // a condition
		"说到底已完成":                             true,  // an idiom, not reported speech
		"刚才已完成":                              true,  // 刚才 is not 才
		"目标写着你 09:12 已经收下了":                  false, // a colon between digits does not cut the phrase
		"原话：已收下":                             false, // a colon after a reporting word
		"it was not merged and 已合并 is wrong": false,
		"it was never 已合并":                   false,
	} {
		if got := unguardedHit(done, s, nil); got != hit {
			t.Errorf("%q: unguarded hit %v, want %v", s, got, hit)
		}
	}
	if unguardedHit(done, "它想已经生效", []string{"想"}) {
		t.Error("an extra guard word did not guard")
	}
}

// Clauses: lines, a bullet's heading, a blank line ending a heading.
func TestClauses(t *testing.T) {
	t.Parallel()
	got := clauseTexts("完成（2）\n· M-41 已收下；M-42 已收下\n- M-43 在等\n\n- 另一组\n1. 第一项。第二句")
	want := []string{"完成（2）", "完成（2） M-41 已收下", "完成（2） M-42 已收下", "完成（2） M-43 在等", "另一组", "第一项", "第二句"}
	if !slices.Equal(got, want) {
		t.Fatalf("clauses %q", got)
	}
	if w := windows("A 卡住。原因不明。B 好了", []string{"A"}); !slices.Equal(w, []string{"A 卡住 原因不明"}) {
		t.Fatalf("windows %q", w)
	}
	if s := segments("claude 落后 9%，codex 超前 24%", []string{"claude"}, []string{"codex"}, false); !slices.Equal(s, []string{"claude 落后 9%，"}) {
		t.Fatalf("segments %q", s)
	}
}

// Lines end where Python's str.splitlines ends them (review 371 B1).
func TestLines(t *testing.T) {
	t.Parallel()
	got := lines("a\u2028b\vc\r\nd\re\u0085f\n")
	if !slices.Equal(got, []string{"a", "b", "c", "d", "e", "f"}) {
		t.Fatalf("lines %q", got)
	}
	if got = lines("x\n\ny"); !slices.Equal(got, []string{"x", "", "y"}) {
		t.Fatalf("lines %q", got)
	}
}

// A pattern means what Python's re.ASCII makes of it (reviews 371 B2, 372
// B2): (?i) folds ASCII letters only, escapes and classes included; \s has
// \v; other runes match only themselves; the answer is not rewritten.
func TestPatternsMeanWhatTheReferenceMeans(t *testing.T) {
	t.Parallel()
	for p, cases := range map[string]map[string]bool{
		`(?i)skip`:         {"SKIP": true, "Skip": true, "ſkip": false},
		`(?i)keep`:         {"KEEP": true, "\u212Aeep": false},
		`(?i)\xe4`:         {"ä": true, "Ä": false},
		`(?i)\x41b`:        {"aB": true, "Ab": true},
		`(?i:edit)x`:       {"EDITx": true, "EDITX": false},
		`(?i)[a-c]+`:       {"aBc": true, "D": false},
		`(?i)[^a-c]`:       {"B": false, "d": true},
		`(?i)(?P<w>ok)`:    {"OK": true},
		`ä`:                {"ä": true, "Ä": false},
		`a\sb`:             {"a\vb": true, "a b": true},
		`a[\s,]b`:          {"a\vb": true, "a,b": true},
		`a\Sb`:             {"axb": true, "a\vb": false},
		`[]x]`:             {"]": true},
		`[^]x]`:            {"y": true, "]": false},
		`[+\-−]?\s*(\d+)%`: {"-5%": true, "−5%": true, "+5%": true},
		`(?i)\bno\b`:       {"No way": true, "nope": false},
	} {
		re, err := compilePy(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		for s, want := range cases {
			if got := re.MatchString(s); got != want {
				t.Errorf("%s on %q: %v, want %v", p, s, got, want)
			}
		}
	}
	for _, p := range []string{`[\S]`, `x(?i)y`, `(?s).`, `\Qa\E`, `\q`, `(a`} {
		if _, err := compilePy(p); err == nil {
			t.Errorf("%s compiled", p)
		}
	}
}

// Checks are written as the reference's json.dumps writes them, whatever the
// escapes of the report (review 371 B3).
func TestPyDumps(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`[["\u0072ule.yaml", "中"]]`: `[["rule.yaml", "\u4e2d"]]`,
		`[["go","-C","api"]]`:       `[["go", "-C", "api"]]`,
		`[["a\"b\\c\n"], 1, null]`:  `[["a\"b\\c\n"], 1, null]`,
		`["😀"]`:                     `["\ud83d\ude00"]`,
		// numbers as Python writes them once read (review 372 B3)
		`[1e2, 1E+02, -0, 1.00, 1e-7, 12, 123456789012.5, 1e16, 0.0001, -0.0]`: `[100.0, 100.0, 0, 1.0, 1e-07, 12, 123456789012.5, 1e+16, 0.0001, -0.0]`,
	} {
		p := "```json\n{\"tasks\": [{\"title\": \"T\", \"text\": \"x\", \"verify\": " + in + "}]}\n```"
		if got := planTasksText(p); got != "T\nx\n"+want {
			t.Errorf("%s: %q", in, got)
		}
	}
	if got := planTasksText("```json\n{\"tasks\": [{\"title\": \"T\", \"text\": \"x\"}]}\n```"); got != "T\nx\n[]" {
		t.Errorf("no verify: %q", got)
	}
}

// The answer is read as written: a rune the reference does not fold stays
// itself for the literal rules too (review 372 B2.1).
func TestTheAnswerIsNotRewritten(t *testing.T) {
	t.Parallel()
	r := &Rule{ID: "m", Kind: "mentions_all", Items: []aliasItem{{Name: "long s", Aliases: []string{"ſ"}}}}
	if err := r.compile(); err != nil {
		t.Fatal(err)
	}
	if ok, why := r.eval("ſ", grading{}); !ok {
		t.Fatalf("a literal alias was not found: %s", why)
	}
}
