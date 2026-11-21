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

// 2026-11-19: CPUThrottlingHigh, an info alert, was first judged false and its
// letters stopped; the duty seat then found it real. Info keeps a letter, one a
// day per alert; an always-letter namespace keeps one each time.
func TestAnInfoAlertWritesOneLetterADay(t *testing.T) {
	r := RuleOf("row", "always-ns")
	at := time.Date(2026, 11, 19, 3, 0, 0, 0, time.UTC)
	info := Alert{Name: "CPUThrottlingHigh", Namespace: "cicd", Severity: "info", Status: "firing"}
	first := r.LetterKey("ev-1", info, at, 1)
	if first == "" || first != r.LetterKey("ev-2", info, at.Add(12*time.Hour), 1) {
		t.Fatalf("info letters within a day: %q", first)
	}
	if first == r.LetterKey("ev-3", info, at.Add(25*time.Hour), 1) {
		t.Fatal("a day later shares the old letter")
	}
	always := info
	always.Namespace = "always-ns"
	if r.LetterKey("ev-4", always, at, 1) == r.LetterKey("ev-5", always, at, 1) {
		t.Fatal("an always-letter namespace lost its letter each time")
	}
}

// Positive control for TestRepeatsShareALetterAndUrgentOnesDoNot: the same
// warning on two machines in one period is two letters (2026-11-20, comm line:
// a second machine's alert used to share the first one's letter, untold).
func TestTheSameAlertOnTwoMachinesIsTwoLetters(t *testing.T) {
	r := RuleOf("alert-firing-to-duty", "")
	at := time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)
	a, _ := Normalize("BifrostHostDiskSpaceLow", "", "warning", "firing")
	b := a
	a.Host, b.Host = "nas", "mini-50"
	if r.LetterKey("ev-1", a, at, 0) == r.LetterKey("ev-2", b, at.Add(time.Minute), 0) {
		t.Fatal("two machines share a letter")
	}
}
