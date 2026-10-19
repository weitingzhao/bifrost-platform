package exam_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/exam"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seatproto"
)

// The real set is not in this repository (an examinee's checkout could read
// it): CHIEF_EXAM_DIR names the directory that holds it and its controls.
func realSet(t *testing.T) (*exam.Set, string) {
	t.Helper()
	dir := os.Getenv("CHIEF_EXAM_DIR")
	if dir == "" {
		t.Skip("set CHIEF_EXAM_DIR to the chief exam's directory to grade its controls")
	}
	s, err := exam.Load(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

type control struct {
	Item   string          `json:"item"`
	Name   string          `json:"name"`
	T0     time.Time       `json:"t0"`
	Expect json.RawMessage `json:"expect"`
	Answer string          `json:"answer"`
}

type expectation struct {
	Fail []string `json:"fail"`
	Soft []string `json:"soft"`
}

// Every control of the set is graded as it expects: exactly its hard rules
// fail, and the soft rules it names flag it (the reference grader's
// verdicts, controls.json).
func TestControlsAsExpected(t *testing.T) {
	t.Parallel()
	s, dir := realSet(t)
	raw, err := os.ReadFile(filepath.Join(dir, "controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cs []control
	if err = json.Unmarshal(raw, &cs); err != nil {
		t.Fatal(err)
	}
	wrong := 0
	for _, c := range cs {
		var want expectation
		if string(c.Expect) != `"pass"` {
			if err = json.Unmarshal(c.Expect, &want); err != nil {
				t.Fatalf("%s %s: expect %s", c.Item, c.Name, c.Expect)
			}
		}
		rs, err := s.Grade(c.Item, c.Answer, c.T0)
		if err != nil {
			t.Fatalf("%s %s: %v", c.Item, c.Name, err)
		}
		var failed, softMissing []string
		for _, r := range rs {
			if r.Hard && !r.OK {
				failed = append(failed, r.Rule)
			}
			if !r.Hard && r.OK && slices.Contains(want.Soft, r.Rule) {
				softMissing = append(softMissing, r.Rule)
			}
		}
		slices.Sort(failed)
		slices.Sort(want.Fail)
		if !slices.Equal(failed, want.Fail) || len(softMissing) > 0 {
			wrong++
			var why []string
			for _, r := range rs {
				if r.Hard && (!r.OK) != slices.Contains(want.Fail, r.Rule) {
					why = append(why, r.Rule+": "+r.Why)
				}
			}
			t.Errorf("%s %q: hard failed %v, want %v; soft not flagged %v\n  %s", c.Item, c.Name, failed, want.Fail, softMissing,
				strings.Join(why, "\n  "))
		}
	}
	t.Logf("%d controls, %d graded otherwise than they expect; set %s", len(cs), wrong, s.SHA256[:12])
}

// Every item of the set renders: its spec is the production spec, with the
// seat's duties, its times filled and no placeholder left.
func TestEveryItemRenders(t *testing.T) {
	t.Parallel()
	s, _ := realSet(t)
	now := time.Date(2026, 10, 20, 2, 7, 30, 0, time.UTC)
	for _, q := range s.Questions {
		for _, it := range q.Items {
			p, err := s.Render(it.ID, exam.T0(it, now))
			if err != nil {
				t.Errorf("%s: %v", it.ID, err)
				continue
			}
			var doc map[string]any
			if err = json.Unmarshal(p.Spec, &doc); err != nil {
				t.Errorf("%s: the spec does not read: %v", it.ID, err)
				continue
			}
			text, _ := doc["text"].(string)
			if !strings.Contains(text, seatproto.Duties) || strings.Contains(text, "{{") || !strings.Contains(text, "2026-10-") {
				t.Errorf("%s: the spec lacks the duties, has a placeholder or no time:\n%.2000s", it.ID, text)
			}
		}
	}
}
