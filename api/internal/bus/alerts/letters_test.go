package alerts

import (
	"testing"
	"time"
)

// Origin: 2026-11-18, the duty seat: 63 alert letters in a day, 46 never read,
// nearly all the same alerts flapping. Repeats share a letter; what it named as
// urgent never does, and a resolved alert writes none.
func TestRepeatsShareALetterAndUrgentOnesDoNot(t *testing.T) {
	at := time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC)
	r := RuleOf("r", "prod-a, prod-b")
	flapping := Alert{Name: "CPUThrottlingHigh", Namespace: "monitoring", Severity: "warning", Status: "firing"}
	first := r.LetterKey("ev-1", flapping, at, 1)
	if first == "" || first != r.LetterKey("ev-2", flapping, at.Add(time.Hour), 1) {
		t.Errorf("a repeat within the window has its own letter: %q", first)
	}
	if first == r.LetterKey("ev-3", flapping, at.Add(7*time.Hour), 1) {
		t.Error("a firing a window later shares the old letter")
	}
	if r.LetterKey("ev-3", flapping, at.Add(7*time.Hour), chronicDays) != r.LetterKey("ev-1", flapping, at, chronicDays) {
		t.Error("an alert that keeps coming back gets more than one letter a day")
	}
	other := flapping
	other.Namespace = "cicd"
	if r.LetterKey("ev-4", other, at, 1) == first {
		t.Error("another namespace shares the letter")
	}
	for _, a := range []Alert{
		{Name: "X", Namespace: "prod-a", Severity: "warning", Status: "firing"},
		{Name: "X", Namespace: "prod-b", Severity: "warning", Status: "firing"},
		{Name: "X", Namespace: "cicd", Severity: "critical", Status: "firing"},
		{Name: "KubeNodePressure", Namespace: "cluster", Severity: "warning", Status: "firing"},
		{Name: "PrometheusBadConfig", Namespace: "monitoring", Severity: "warning", Status: "firing"},
		{Name: "KubePodCrashLooping", Namespace: "monitoring", Severity: "warning", Status: "firing"},
	} {
		if r.LetterKey("ev-5", a, at, 9) == r.LetterKey("ev-6", a, at, 9) {
			t.Errorf("%+v shares a letter", a)
		}
	}
	resolved := flapping
	resolved.Status = "resolved"
	if k := r.LetterKey("ev-7", resolved, at, 1); k != "" {
		t.Errorf("a resolved alert has a letter: %q", k)
	}
}

// 2026-11-19, the first false alarm the problem loop closed: CPUThrottlingHigh, an
// info alert, woke the duty seat. Info gets no letter, unless the namespace or
// the name is one that always does.
func TestAnInfoAlertWritesNoLetter(t *testing.T) {
	r := RuleOf("row", "always-ns")
	at := time.Date(2026, 11, 19, 3, 0, 0, 0, time.UTC)
	if k := r.LetterKey("ev-1", Alert{Name: "CPUThrottlingHigh", Namespace: "cicd", Severity: "info", Status: "firing"}, at, 1); k != "" {
		t.Fatalf("info alert got letter %q", k)
	}
	if k := r.LetterKey("ev-2", Alert{Name: "CPUThrottlingHigh", Namespace: "always-ns", Severity: "info", Status: "firing"}, at, 1); k == "" {
		t.Fatal("an always-letter namespace lost its letter")
	}
	if k := r.LetterKey("ev-3", Alert{Name: "X", Namespace: "cicd", Severity: "warning", Status: "firing"}, at, 1); k == "" {
		t.Fatal("a warning lost its letter")
	}
}
