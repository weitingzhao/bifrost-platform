package recorderimport_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
)

func digest(body string) string {
	s := sha256.Sum256([]byte(body))
	return hex.EncodeToString(s[:])
}

// chain seals bodies the way events.py does: each a canonical object with
// v, seq and prev_sha256 filled in.
func chain(t *testing.T, work string, events ...map[string]any) []string {
	t.Helper()
	var lines []string
	prev := ""
	for i, e := range events {
		e["v"], e["seq"], e["prev_sha256"], e["by"], e["at"] = 2, i+1, prev, "coordinator", 1791952517.47163+float64(i)
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		prev = digest(body)
		lines = append(lines, prev+" "+body)
	}
	return lines
}

func request(work string, lines []string) recorderimport.Request {
	return recorderimport.Request{Format: recorderimport.Format, Lines: lines,
		Source: recorderimport.Source{WorkID: work, File: work + "/events.jsonl", SHA256: digest(strings.Join(lines, "\n") + "\n")}}
}

func TestReadAndCheck(t *testing.T) {
	t.Parallel()
	lines := chain(t, "W-31",
		map[string]any{"type": "work_opened", "work_id": "W-31", "title": "review 141"},
		map[string]any{"type": "step_started", "step_id": "12f12f10", "command": "sh -c 'cd /Users/someone/Desktop/stocks && codex exec -C /tmp/x'"},
		map[string]any{"type": "step_finished", "step_id": "12f12f10", "exit": 0})
	log, err := recorderimport.Read(request("W-31", lines))
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Records) != 3 || log.Masked() != 1 || !log.Records[1].Masked || log.Records[0].Masked {
		t.Fatalf("records %+v", log.Records)
	}
	if strings.Contains(log.Records[1].Body, "/Users/") || strings.Contains(log.Records[1].Body, "/tmp/") ||
		!strings.Contains(log.Records[1].Body, "<Users>/someone") {
		t.Fatalf("masked body %s", log.Records[1].Body)
	}
	if log.Records[1].Digest != strings.SplitN(lines[1], " ", 2)[0] {
		t.Fatal("a masked record lost its original digest")
	}
	if got := log.Types(); got["step_started"] != 1 || got["work_opened"] != 1 {
		t.Fatalf("types %v", got)
	}
	// Kept, read back and checked again.
	raw, err := json.Marshal(log)
	if err != nil {
		t.Fatal(err)
	}
	var back recorderimport.Log
	if err = json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if err = recorderimport.Check(back); err != nil {
		t.Fatalf("the kept log does not check: %v", err)
	}
	// Review 402 C1: a masked flag set on an unmasked record whose header
	// stays whole passes Check by design; the kept log's own digest (its
	// artifact id, read back at the decision) is what holds its bodies.
	{
		l := back
		l.Records = append([]recorderimport.Record(nil), back.Records...)
		l.Records[2].Masked = true
		l.Records[2].Body = strings.Replace(l.Records[2].Body, `"exit":0`, `"exit":1`, 1)
		if err := recorderimport.Check(l); err != nil {
			t.Fatalf("Check's contract changed (a masked body is not hash-checked): %v", err)
		}
	}
	// A kept log changed after it was read.
	for name, change := range map[string]func(*recorderimport.Log){
		"an unmasked body": func(l *recorderimport.Log) {
			l.Records[2].Body = strings.Replace(l.Records[2].Body, `"exit":0`, `"exit":1`, 1)
		},
		"a record dropped": func(l *recorderimport.Log) { l.Records = append(l.Records[:1], l.Records[2]) },
		"a masked link": func(l *recorderimport.Log) {
			l.Records[1].Body = strings.Replace(l.Records[1].Body, `"seq":2`, `"seq":5`, 1)
		},
		"a path put back": func(l *recorderimport.Log) {
			l.Records[1].Body = strings.Replace(l.Records[1].Body, "<Users>/", "/Users/", 1)
		},
		"masked claimed, no header": func(l *recorderimport.Log) { l.Records[2].Masked = true; l.Records[2].Body = `{"v":2}` },
		"another work":              func(l *recorderimport.Log) { l.Source.WorkID = "W-32" },
	} {
		l := back
		l.Records = append([]recorderimport.Record(nil), back.Records...)
		change(&l)
		if err := recorderimport.Check(l); err == nil {
			t.Errorf("%s: the kept log still checks", name)
		}
	}
}

func TestReadRefusals(t *testing.T) {
	t.Parallel()
	good := chain(t, "td-336",
		map[string]any{"type": "work_opened", "work_id": "td-336"},
		map[string]any{"type": "step_declared", "step_id": "a1"},
		map[string]any{"type": "step_finished", "step_id": "a1"})
	bodyOf := func(l string) string { return strings.SplitN(l, " ", 2)[1] }
	reseal := func(body string) string { return digest(body) + " " + body }
	cases := map[string]struct {
		req  func() recorderimport.Request
		want string
	}{
		"format": {func() recorderimport.Request {
			r := request("td-336", good)
			r.Format = "agentrt.plan_import/v1"
			return r
		}, "format"},
		"empty": {func() recorderimport.Request { return request("td-336", nil) }, "at least one line"},
		"work id": {func() recorderimport.Request { r := request("td-336", good); r.Source.WorkID = "../x"; return r },
			"work_id"},
		"local file": {func() recorderimport.Request {
			r := request("td-336", good)
			r.Source.File = "/Users/someone/agent-work/recorder-root/td-336/events.jsonl"
			return r
		}, "relative path"},
		"dot dot": {func() recorderimport.Request { r := request("td-336", good); r.Source.File = "td-336/../x"; return r }, "relative path"},
		"file sha": {func() recorderimport.Request {
			r := request("td-336", good)
			r.Source.SHA256 = digest("other")
			return r
		}, "hash to"},
		"digest": {func() recorderimport.Request {
			l := append([]string(nil), good...)
			l[1] = digest("x") + " " + bodyOf(l[1])
			return request("td-336", l)
		}, "line 2: the body does not hash"},
		"no space": {func() recorderimport.Request {
			l := append([]string(nil), good...)
			l[0] = strings.Replace(l[0], " ", "", 1)
			return request("td-336", l)
		}, "line 1: not a digest"},
		"seq gap": {func() recorderimport.Request {
			l := append([]string(nil), good...)
			l[2] = reseal(strings.Replace(bodyOf(l[2]), `"seq":3`, `"seq":4`, 1))
			return request("td-336", l)
		}, "line 3: seq is 4"},
		"prev": {func() recorderimport.Request {
			l := append([]string(nil), good...)
			l[2] = reseal(strings.Replace(bodyOf(l[2]), digest(bodyOf(l[1])), digest("x"), 1))
			return request("td-336", l)
		}, "line 3: prev_sha256"},
		"v": {func() recorderimport.Request {
			l := []string{reseal(strings.Replace(bodyOf(good[0]), `"v":2`, `"v":1`, 1))}
			return request("td-336", l)
		}, "line 1: v is 1"},
		"duplicate key": {func() recorderimport.Request {
			l := []string{reseal(strings.Replace(bodyOf(good[0]), `{`, `{"type":"x",`, 1))}
			return request("td-336", l)
		}, "given twice"},
		"other work": {func() recorderimport.Request {
			l := []string{reseal(strings.Replace(bodyOf(good[0]), `"work_id":"td-336"`, `"work_id":"td-337"`, 1))}
			return request("td-336", l)
		}, "is not the log's"},
		"workspace": {func() recorderimport.Request {
			l := chain(t, "td-336", map[string]any{"type": "note", "text": "see tasks/t_01M5AHP9Q7G4RWDSW5DGR5DSY2/a1/x"})
			return request("td-336", l)
		}, "workspace"},
		"not an object": {func() recorderimport.Request { return request("td-336", []string{reseal(`[1,2]`)}) }, "not a recorder event"},
	}
	for name, c := range cases {
		if _, err := recorderimport.Read(c.req()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v; want %q", name, err, c.want)
		}
	}
}

// The real logs, when RECORDER_ROOT names the recorder's root (this Mac:
// ~/agent-work/recorder-root): every log reads, its chain holds, and its kept
// form checks. The logs are not in any repository.
func TestRealLogs(t *testing.T) {
	t.Parallel()
	root := os.Getenv("RECORDER_ROOT")
	if root == "" {
		t.Skip("set RECORDER_ROOT to the recorder's root to read the real logs")
	}
	files, err := filepath.Glob(filepath.Join(root, "*", "events.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no logs under %s: %v", root, err)
	}
	total, masked := 0, 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		work := filepath.Base(filepath.Dir(f))
		lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
		log, err := recorderimport.Read(request(work, lines))
		if err != nil {
			t.Errorf("%s: %v", work, err)
			continue
		}
		if err = recorderimport.Check(log); err != nil {
			t.Errorf("%s: the kept log does not check: %v", work, err)
		}
		total += len(log.Records)
		masked += log.Masked()
	}
	t.Logf("%d logs, %d records, %d masked", len(files), total, masked)
}

// sealOne is one sealed line of a body given as text.
func sealOne(body string) []string { return []string{digest(body) + " " + body} }

// Review 402 A1: the chain's fields are read by their exact names; "V",
// "SEQ" and the like, which Go's struct decoding would take, are other keys.
// B1: a path in a key or in a field the import reads is refused, never
// masked; B2: a path written with escapes is masked like any other.
func TestReadExactFieldsAndValuesOnly(t *testing.T) {
	t.Parallel()
	refused := map[string]struct{ body, want string }{
		"aliases": {`{"v":1,"V":2,"seq":99,"SEQ":1,"prev_sha256":"broken","PREV_SHA256":"","type":"note","work_id":"W-32","WORK_ID":"W-31"}`,
			"v is 1"},
		"only upper case": {`{"V":2,"SEQ":1,"PREV_SHA256":"","TYPE":"note"}`, "no v"},
		"seq a string":    {`{"v":2,"seq":"1","prev_sha256":"","type":"note"}`, "seq is not of its type"},
		"a path key":      {`{"v":2,"seq":1,"prev_sha256":"","type":"note","/tmp/x":"first","<tmp>/x":"second"}`, "a key has a path"},
		"a path type":     {`{"v":2,"seq":1,"prev_sha256":"","type":"/tmp/thing"}`, "its type has a path"},
		"an escaped key":  {`{"v":2,"seq":1,"prev_sha256":"","type":"note","\u002fUsers\u002fx":1}`, "a key has a path"},
	}
	for name, c := range refused {
		if _, err := recorderimport.Read(request("W-31", sealOne(c.body))); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v; want %q", name, err, c.want)
		}
	}
	escaped := `{"at":1791936767.5,"text":"\u002fUsers\u002falice\u002fx and \/home\/bob","seq":1,"prev_sha256":"","type":"note","v":2}`
	log, err := recorderimport.Read(request("W-31", sealOne(escaped)))
	if err != nil {
		t.Fatal(err)
	}
	r := log.Records[0]
	var got map[string]any
	if err = json.Unmarshal([]byte(r.Body), &got); err != nil {
		t.Fatal(err)
	}
	if !r.Masked || got["text"] != "<Users>/alice/x and <home>/bob" || !strings.Contains(r.Body, `"at":1791936767.5`) || r.Digest != digest(escaped) {
		t.Fatalf("escaped paths kept as %s (masked %v)", r.Body, r.Masked)
	}
	if err = recorderimport.Check(log); err != nil {
		t.Fatal(err)
	}
}
