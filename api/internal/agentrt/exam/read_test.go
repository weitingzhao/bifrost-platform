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

// A pattern means what Python's re.ASCII makes of it (review 371 B2): (?i)
// folds ASCII letters only, \s has \v; a cased non-ASCII letter is refused.
func TestPatternsMeanWhatTheReferenceMeans(t *testing.T) {
	t.Parallel()
	skip, err := compilePy(`(?i)skip`)
	if err != nil {
		t.Fatal(err)
	}
	if skip.MatchString(asciiFold.Replace("ſkip")) || skip.MatchString(asciiFold.Replace("\u212Aeep")) || !skip.MatchString("SKIP") {
		t.Error("(?i) folds otherwise than re.ASCII")
	}
	for p, s := range map[string]string{`a\sb`: "a\vb", `a[\s,]b`: "a\vb", `a\Sb`: "axb", `[]x]`: "]", `[^]x]`: "y"} {
		re, err := compilePy(p)
		if err != nil || !re.MatchString(s) {
			t.Errorf("%s on %q: %v", p, s, err)
		}
	}
	if re, _ := compilePy(`a\Sb`); re.MatchString("a\vb") {
		t.Error(`\S took \v`)
	}
	for _, p := range []string{`(?i)ärger`, `[\S]`} {
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
