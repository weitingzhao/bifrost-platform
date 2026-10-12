package releasefreeze

import (
	"bufio"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// oracleRE is the independent recogniser. It is written from the productions:
// \A, an explicit ASCII whitespace class, no $, no \s. A match counts only when
// it consumes every byte.
var oracleRE = regexp.MustCompile(oraclePattern())

func oracleMatch(b []byte) bool {
	loc := oracleRE.FindIndex(b)
	return loc != nil && loc[0] == 0 && loc[1] == len(b)
}

func oraclePattern() string {
	W := `[ \t\r\n]*`
	q := func(s string) string { return `"` + regexp.QuoteMeta(s) + `"` }
	rv := q("resourceVersion") + W + `:` + W + `"` + `[0-9]{1,32}` + `"`
	ct := q("continue") + W + `:` + W + `""`
	meta := `\{` + W + `(?:` + rv + W + `\}|` + ct + W + `,` + W + rv + W + `\}|` + rv + W + `,` + W + ct + W + `\})`
	av := W + q("apiVersion") + W + `:` + W + q("release.bifrost.io/v1")
	kd := W + q("kind") + W + `:` + W + q("FreezeList")
	it := W + q("items") + W + `:` + W + `\[` + W + `\]`
	md := W + q("metadata") + W + `:` + W + meta
	members := []string{av, kd, md, it}
	var alts string
	perms := permute(len(members))
	for i, p := range perms {
		if i > 0 {
			alts += `|`
		}
		alts += `\{` + members[p[0]] + W + `,` + members[p[1]] + W + `,` + members[p[2]] + W + `,` + members[p[3]] + W + `\}`
	}
	return `\A` + W + `(?:` + alts + `)` + W
}

func permute(n int) [][]int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	var out [][]int
	var rec func(int)
	rec = func(k int) {
		if k == n {
			row := append([]int(nil), idx...)
			out = append(out, row)
			return
		}
		for i := k; i < n; i++ {
			idx[k], idx[i] = idx[i], idx[k]
			rec(k + 1)
			idx[k], idx[i] = idx[i], idx[k]
		}
	}
	rec(0)
	return out
}

type genCase struct {
	body   []byte
	member bool
	group  string
}

func generateCases() []genCase {
	var out []genCase
	for _, body := range compactMembers() {
		out = append(out, genCase{body: body, member: true, group: "member"})
	}
	for _, body := range whitespaceMembers() {
		out = append(out, genCase{body: body, member: true, group: "member"})
	}
	base, marks := skeleton("")
	if len(base) != 118 || len(marks) != 27 {
		panic("skeleton")
	}
	for i := 0; i < len(base); i++ {
		b := append([]byte(nil), base...)
		b[i] = 0xFF
		out = append(out, genCase{body: b, member: false, group: "near"})
	}
	for _, b := range slotInserts(base) {
		out = append(out, genCase{body: b, member: false, group: "near"})
	}
	out = append(out, genCase{body: digits33(base), member: false, group: "near"})
	for _, b := range slashInserts(base) {
		out = append(out, genCase{body: b, member: false, group: "near"})
	}
	for _, b := range dupKeys(base) {
		out = append(out, genCase{body: b, member: false, group: "near"})
	}
	for _, at := range marks {
		b := make([]byte, 0, len(base)+1)
		b = append(b, base[:at]...)
		b = append(b, 'A')
		b = append(b, base[at:]...)
		out = append(out, genCase{body: b, member: false, group: "near"})
	}
	for _, b := range randomBodies() {
		out = append(out, genCase{body: b, member: false, group: "random"})
	}
	return out
}

func compactMembers() [][]byte {
	var out [][]byte
	for _, perm := range permute(4) {
		for form := 0; form < 3; form++ {
			for _, n := range []int{1, 32} {
				out = append(out, compactBody(perm, form, n))
			}
		}
	}
	return out
}

func compactBody(perm []int, form, digits int) []byte {
	rv := string(bytesRepeat('1', digits))
	R := `"resourceVersion":"` + rv + `"`
	C := `"continue":""`
	var meta string
	switch form {
	case 0:
		meta = "{" + R + "}"
	case 1:
		meta = "{" + C + "," + R + "}"
	default:
		meta = "{" + R + "," + C + "}"
	}
	parts := [4]string{
		`"apiVersion":"release.bifrost.io/v1"`,
		`"kind":"FreezeList"`,
		`"metadata":` + meta,
		`"items":[]`,
	}
	return []byte("{" + parts[perm[0]] + "," + parts[perm[1]] + "," + parts[perm[2]] + "," + parts[perm[3]] + "}")
}

func bytesRepeat(c byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return b
}

func whitespaceMembers() [][]byte {
	base, marks := skeleton("1")
	var out [][]byte
	for _, at := range marks {
		for _, c := range []byte{0x20, 0x09, 0x0A, 0x0D} {
			b := make([]byte, 0, len(base)+1)
			b = append(b, base[:at]...)
			b = append(b, c)
			b = append(b, base[at:]...)
			out = append(out, b)
		}
	}
	return out
}

// skeleton is the compact measured order with resourceVersion of d ones (empty d means "1").
// Marks are the 27 W positions, left to right.
func skeleton(digits string) ([]byte, []int) {
	if digits == "" {
		digits = "1"
	}
	var buf []byte
	var marks []int
	w := func() { marks = append(marks, len(buf)) }
	s := func(x string) { buf = append(buf, x...) }
	q := func(x string) { s(`"` + x + `"`) }
	mAV := func() {
		w()
		q("apiVersion")
		w()
		s(":")
		w()
		q("release.bifrost.io/v1")
	}
	mIT := func() {
		w()
		q("items")
		w()
		s(":")
		w()
		s("[")
		w()
		s("]")
	}
	mKD := func() {
		w()
		q("kind")
		w()
		s(":")
		w()
		q("FreezeList")
	}
	C := func() { q("continue"); w(); s(":"); w(); q("") }
	R := func() { q("resourceVersion"); w(); s(":"); w(); q(digits) }
	meta := func() {
		s("{")
		w()
		C()
		w()
		s(",")
		w()
		R()
		w()
		s("}")
	}
	mMD := func() {
		w()
		q("metadata")
		w()
		s(":")
		w()
		meta()
	}
	w()
	s("{")
	mAV()
	w()
	s(",")
	mIT()
	w()
	s(",")
	mKD()
	w()
	s(",")
	mMD()
	w()
	s("}")
	w()
	return buf, marks
}

func slotInserts(base []byte) [][]byte {
	// Five top slots and three meta slots, each gaining a comma and "z":"".
	topEnd := len(base) - 1 // the final }
	metaKey := []byte(`"metadata":`)
	metaAt := indexOf(base, metaKey)
	metaBrace := metaAt + len(metaKey)
	metaEnd := topEnd - 1 // '}' of meta, just before top's '}'
	metaComma := indexOf(base[metaBrace:], []byte(",")) + metaBrace
	topCommas := topLevelCommas(base, metaBrace, metaEnd)
	type spec struct {
		at   int
		text string
	}
	specs := []spec{
		{1, `"z":"",`},
		{topCommas[0] + 1, `"z":"",`},
		{topCommas[1] + 1, `"z":"",`},
		{topCommas[2] + 1, `"z":"",`},
		{topEnd, `,"z":""`},
		{metaBrace + 1, `"z":"",`},
		{metaComma + 1, `"z":"",`},
		{metaEnd, `,"z":""`},
	}
	var out [][]byte
	for _, sp := range specs {
		b := make([]byte, 0, len(base)+len(sp.text))
		b = append(b, base[:sp.at]...)
		b = append(b, sp.text...)
		b = append(b, base[sp.at:]...)
		out = append(out, b)
	}
	return out
}

func topLevelCommas(base []byte, metaBrace, metaEnd int) []int {
	var out []int
	for i, c := range base {
		if c == ',' && (i < metaBrace || i > metaEnd) {
			out = append(out, i)
		}
	}
	return out
}

func indexOf(b, sub []byte) int {
	for i := 0; i+len(sub) <= len(b); i++ {
		ok := true
		for j := range sub {
			if b[i+j] != sub[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func digits33(base []byte) []byte {
	old := []byte(`"resourceVersion":"1"`)
	neu := []byte(`"resourceVersion":"` + string(bytesRepeat('1', 33)) + `"`)
	at := indexOf(base, old)
	b := make([]byte, 0, len(base)+32)
	b = append(b, base[:at]...)
	b = append(b, neu...)
	b = append(b, base[at+len(old):]...)
	return b
}

func slashInserts(base []byte) [][]byte {
	// 6 keys and 4 string values: a backslash just inside each opening quote.
	keys := []string{`"apiVersion"`, `"items"`, `"kind"`, `"metadata"`, `"continue"`, `"resourceVersion"`}
	vals := []string{`"release.bifrost.io/v1"`, `"FreezeList"`, `""`, `"1"`}
	var ats []int
	from := 0
	for _, k := range keys {
		at := indexFrom(base, []byte(k), from)
		ats = append(ats, at+1) // inside the quotes
		from = at + len(k)
	}
	from = 0
	// values occur after their keys; locate in order, skipping keys already passed by scanning quotes that are values.
	valSubs := [][]byte{[]byte(`"release.bifrost.io/v1"`), []byte(`"FreezeList"`)}
	for _, v := range valSubs {
		at := indexOf(base, v)
		ats = append(ats, at+1)
	}
	// continue's empty string is the first `""` after "continue"
	cAt := indexOf(base, []byte(`"continue"`))
	empty := indexFrom(base, []byte(`""`), cAt)
	ats = append(ats, empty+1)
	rv := indexOf(base, []byte(`"resourceVersion"`))
	one := indexFrom(base, []byte(`"1"`), rv)
	ats = append(ats, one+1)
	_ = vals
	var out [][]byte
	for _, at := range ats {
		b := make([]byte, 0, len(base)+1)
		b = append(b, base[:at]...)
		b = append(b, '\\')
		b = append(b, base[at:]...)
		out = append(out, b)
	}
	return out
}

func indexFrom(b, sub []byte, from int) int {
	if from < 0 {
		from = 0
	}
	i := indexOf(b[from:], sub)
	if i < 0 {
		return -1
	}
	return from + i
}

func dupKeys(base []byte) [][]byte {
	members := []string{
		`"apiVersion":"release.bifrost.io/v1"`,
		`"items":[]`,
		`"kind":"FreezeList"`,
		`"metadata":{"continue":"","resourceVersion":"1"}`,
	}
	var out [][]byte
	topEnd := len(base) - 1
	for _, m := range members {
		text := "," + m
		b := make([]byte, 0, len(base)+len(text))
		b = append(b, base[:topEnd]...)
		b = append(b, text...)
		b = append(b, base[topEnd:]...)
		out = append(out, b)
	}
	metaEnd := topEnd - 1
	for _, m := range []string{`"continue":""`, `"resourceVersion":"1"`} {
		text := "," + m
		b := make([]byte, 0, len(base)+len(text))
		b = append(b, base[:metaEnd]...)
		b = append(b, text...)
		b = append(b, base[metaEnd:]...)
		out = append(out, b)
	}
	return out
}

func randomBodies() [][]byte {
	s := 310101
	total := 255 * 256 / 2
	stream := make([]byte, total)
	for i := 0; i < total; i++ {
		s = (1103515245*s + 12345) % (1 << 31)
		stream[i] = byte((s >> 16) % 256)
	}
	out := make([][]byte, 256)
	at := 0
	for n := 0; n < 256; n++ {
		out[n] = append([]byte(nil), stream[at:at+n]...)
		at += n
	}
	return out
}

func handBodies() []struct {
	name   string
	body   string
	member bool
} {
	return []struct {
		name   string
		body   string
		member bool
	}{
		{"measured", "{\"apiVersion\":\"release.bifrost.io/v1\",\"items\":[],\"kind\":\"FreezeList\",\"metadata\":{\"continue\":\"\",\"resourceVersion\":\"84135567\"}}\n", true},
		{"two-resourceVersion", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":"1","resourceVersion":"2"},"items":[]}`, false},
		{"two-continue", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"continue":"","continue":""},"items":[]}`, false},
		{"continue-rv-continue", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"continue":"","resourceVersion":"1","continue":""},"items":[]}`, false},
		{"rv-continue-rv", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":"1","continue":"","resourceVersion":"2"},"items":[]}`, false},
		{"only-continue", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"continue":""},"items":[]}`, false},
		{"nan", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{},"items":[],"x":NaN}`, false},
		{"infinity", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{},"items":[],"x":Infinity}`, false},
		{"remainingItemCount", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":"1","remainingItemCount":1},"items":[]}`, false},
		{"fifth", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":"1"},"items":[],"z":""}`, false},
		{"dup-member", `{"apiVersion":"release.bifrost.io/v1","apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":"1"}}`, false},
		{"continue-nonempty", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"continue":"x","resourceVersion":"1"},"items":[]}`, false},
		{"digits-33", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":"111111111111111111111111111111111"},"items":[]}`, false},
		{"digits-0", `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":""},"items":[]}`, false},
		{"trailing", "{\"apiVersion\":\"release.bifrost.io/v1\",\"kind\":\"FreezeList\",\"metadata\":{\"resourceVersion\":\"1\"},\"items\":[]}A", false},
	}
}

func TestGenerated(t *testing.T) {
	start := time.Now()
	cases := generateCases()
	t.Logf("generator seconds %.3f", time.Since(start).Seconds())
	if len(cases) != 678 {
		t.Fatalf("cases %d", len(cases))
	}
	var members, near, random, accept int
	for _, c := range cases {
		switch c.group {
		case "member":
			members++
		case "near":
			near++
		case "random":
			random++
		}
		got := MemberOfL(c.body)
		ora := oracleMatch(c.body)
		if got != ora {
			t.Fatalf("recogniser and oracle differ group %s member %v len %d", c.group, c.member, len(c.body))
		}
		if got != c.member {
			t.Fatalf("group %s want member %v len %d", c.group, c.member, len(c.body))
		}
		if got {
			accept++
			if DecideHTTP(200, c.body).Verdict != VerdictClear {
				t.Fatal("member was not clear")
			}
		} else if DecideHTTP(200, c.body).Verdict == VerdictClear {
			t.Fatal("non-member was clear")
		}
	}
	t.Logf("cases %d", len(cases))
	t.Logf("members %d", members)
	t.Logf("near %d", near)
	t.Logf("random %d", random)
	t.Logf("seed %d", 310101)
	if members != 252 || near != 170 || random != 256 || accept != 252 {
		t.Fatalf("counts members %d near %d random %d accept %d", members, near, random, accept)
	}
	bodies := make([][]byte, len(cases))
	want := make([]bool, len(cases))
	for i, c := range cases {
		bodies[i] = c.body
		want[i] = c.member
	}
	agreePython(t, bodies, want)
}

func TestHandVectors(t *testing.T) {
	for _, h := range handBodies() {
		body := []byte(h.body)
		if MemberOfL(body) != h.member || oracleMatch(body) != h.member {
			t.Fatalf("%s member scanner %v oracle %v", h.name, MemberOfL(body), oracleMatch(body))
		}
		got := DecideHTTP(200, body)
		if h.member && got.Verdict != VerdictClear {
			t.Fatalf("%s not clear", h.name)
		}
		if !h.member && got.Verdict == VerdictClear {
			t.Fatalf("%s was clear", h.name)
		}
	}
	bodies := make([][]byte, 0, len(handBodies()))
	want := make([]bool, 0, len(handBodies()))
	for _, h := range handBodies() {
		bodies = append(bodies, []byte(h.body))
		want = append(want, h.member)
	}
	agreePython(t, bodies, want)
}

const pyMemberSrc = `
import base64, sys
sys.path.insert(0, sys.argv[1])
import freeze_gate
for line in sys.stdin:
    line = line.strip()
    body = base64.standard_b64decode(line) if line else b""
    sys.stdout.write("1\n" if freeze_gate.member_of_l(body) else "0\n")
`

func agreePython(t *testing.T, bodies [][]byte, want []bool) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "bifrost-trade-infra", "scripts", "release"))
	if _, err := os.Stat(filepath.Join(dir, "freeze_gate.py")); err != nil {
		if os.Getenv("BIFROST_CI") != "" {
			t.Fatalf("sibling freeze_gate.py missing: %v", err)
		}
		t.Skipf("sibling freeze_gate.py absent: %v", err)
	}
	if _, err := exec.LookPath("python3"); err != nil {
		if os.Getenv("BIFROST_CI") != "" {
			t.Fatal("python3 absent")
		}
		t.Skip("python3 absent")
	}
	cmd := exec.Command("python3", "-u", "-c", pyMemberSrc, dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for _, body := range bodies {
		if _, err := stdin.Write([]byte(base64.StdEncoding.EncodeToString(body) + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	_ = stdin.Close()
	sc := bufio.NewScanner(stdout)
	for i := range bodies {
		if !sc.Scan() {
			t.Fatalf("python closed at %d: %v %s", i, sc.Err(), stderr.String())
		}
		got := sc.Text() == "1"
		if got != want[i] || got != MemberOfL(bodies[i]) {
			t.Fatalf("python body %d clear-member %v go %v want %v", i, got, MemberOfL(bodies[i]), want[i])
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("python: %v %s", err, stderr.String())
	}
}

func TestStatusIsNotAList(t *testing.T) {
	body := EmptyListBody()
	for _, status := range []int{404, 403, 500} {
		if DecideHTTP(status, body).Verdict == VerdictClear {
			t.Fatalf("HTTP %d was clear", status)
		}
	}
}

// EmptyListBody is the measured empty list with the freeze literals and a trailing LF.
func EmptyListBody() []byte {
	return []byte("{\"apiVersion\":\"release.bifrost.io/v1\",\"items\":[],\"kind\":\"FreezeList\",\"metadata\":{\"continue\":\"\",\"resourceVersion\":\"84135567\"}}\n")
}
