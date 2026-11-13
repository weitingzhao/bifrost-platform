// Package unitexec is the platform's two dealings with a unit task (CI design
// v4, landing stage 2 block 2-C): dispatch one, and receive one's result.
//
// This file is the receiving half, and it is written against what the
// cluster returned for four real runs on 2026-11-13 (testdata/
// real_taskruns.json: one passed, one with a red test, one that did not
// compile, one whose image could not be pulled):
//
//   - a red test and a package that does not build are the same to Tekton
//     (condition False, reason Failed, the same message); only the task's
//     fail-class result tells them apart;
//   - a run whose image could not be pulled ends in seconds, with a reason of
//     its own, no result at all (not even the commit), and no pod left;
//   - the sidecars' exit codes say nothing (postgres ends 137 on a pass).
//
// So a run is passed, test or build only when it says so itself; everything
// else is infra: the content was not judged, and Tekton's words are kept.
package unitexec

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// What a unit TaskRun carries about itself (landing stage 2 §4.0). The unit's
// name and the whole key are annotations: a name has slashes and a key is
// longer than a label value may be.
const (
	LabelPlan      = "bifrost.io/unit-plan"
	LabelRepo      = "bifrost.io/unit-repo"
	LabelTask      = "bifrost.io/unit-task"
	LabelKey       = "bifrost.io/unit-key"
	LabelAttempt   = "bifrost.io/unit-attempt"
	AnnotationUnit = "bifrost.io/unit"
	AnnotationKey  = "bifrost.io/unit-input-key"
)

// TaskRun is the part of a Tekton TaskRun the receiver reads.
type TaskRun struct {
	Metadata struct {
		Name              string            `json:"name"`
		UID               string            `json:"uid"`
		CreationTimestamp *time.Time        `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
		Annotations       map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Params []NameValue `json:"params"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
		StartTime      *time.Time  `json:"startTime"`
		CompletionTime *time.Time  `json:"completionTime"`
		Results        []NameValue `json:"results"`
		Steps          []struct {
			Name       string `json:"name"`
			Terminated *struct {
				StartedAt  *time.Time `json:"startedAt"`
				FinishedAt *time.Time `json:"finishedAt"`
			} `json:"terminated"`
		} `json:"steps"`
	} `json:"status"`
}

// NameValue is a Tekton parameter or result with a string value.
type NameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

const messageMax = 1000

var (
	sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sha1Hex   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Receive reads a unit TaskRun into the row that records it. done is false
// while the run has not finished. An error is a run that is not a unit
// task's, or that names itself wrongly: nothing is recorded for it.
func Receive(tr TaskRun) (row store.UnitRun, done bool, err error) {
	labels, ann := tr.Metadata.Labels, tr.Metadata.Annotations
	row = store.UnitRun{
		Plan: labels[LabelPlan], Repo: labels[LabelRepo], Task: labels[LabelTask],
		Unit: ann[AnnotationUnit], InputKey: ann[AnnotationKey],
		RunName: tr.Metadata.Name, RunUID: tr.Metadata.UID,
		ShardTotal: 1, Attempt: 1,
	}
	if row.Plan == "" || row.Repo == "" || row.Task == "" || row.Unit == "" || row.RunName == "" || row.RunUID == "" {
		return row, false, fmt.Errorf("unitexec: TaskRun %s is not a unit task's: plan, repo, task, unit, name and uid are all needed", tr.Metadata.Name)
	}
	if !sha256Hex.MatchString(row.InputKey) || !strings.HasPrefix(row.InputKey, labels[LabelKey]) || labels[LabelKey] == "" {
		return row, false, fmt.Errorf("unitexec: TaskRun %s: the key annotation is not a sha256 its key label begins", row.RunName)
	}
	params := values(tr.Spec.Params)
	if row.CommitSHA = params["revision"]; !sha1Hex.MatchString(row.CommitSHA) {
		return row, false, fmt.Errorf("unitexec: TaskRun %s: revision %q is not a commit", row.RunName, row.CommitSHA)
	}
	for name, into := range map[string]*int{"shard-index": &row.ShardIndex, "shard-total": &row.ShardTotal} {
		if v, ok := params[name]; ok {
			if *into, err = strconv.Atoi(v); err != nil {
				return row, false, fmt.Errorf("unitexec: TaskRun %s: %s %q", row.RunName, name, v)
			}
		}
	}
	if v := labels[LabelAttempt]; v != "" {
		if row.Attempt, err = strconv.Atoi(v); err != nil {
			return row, false, fmt.Errorf("unitexec: TaskRun %s: attempt %q", row.RunName, v)
		}
	}

	status, reason, message := "", "", ""
	for _, c := range tr.Status.Conditions {
		if c.Type == "Succeeded" {
			status, reason, message = c.Status, c.Reason, c.Message
		}
	}
	if status != "True" && status != "False" {
		return row, false, nil
	}
	row.Reason, row.Message = reason, cut(message, messageMax)
	row.StartedAt, row.CompletedAt = tr.Status.StartTime, tr.Status.CompletionTime
	results := values(tr.Status.Results)
	_, judged := results["fail-class"]
	switch {
	case status == "True":
		row.Class = store.UnitRunPassed
	case reason == "Failed" && judged && results["fail-class"] == "test":
		row.Class = store.UnitRunTest
	case reason == "Failed" && judged && results["fail-class"] == "build":
		row.Class = store.UnitRunBuild
	default:
		row.Class = store.UnitRunInfra
	}
	if row.Class == store.UnitRunInfra {
		// Nothing the run reported is taken: it did not judge the content,
		// and a run that never started reports step times that are not its own.
		return row, true, nil
	}

	if code, convErr := strconv.Atoi(results["exit-code"]); convErr == nil {
		row.ExitCode = &code
	}
	if seconds, convErr := strconv.Atoi(results["duration-seconds"]); convErr == nil && seconds >= 0 {
		row.RunSeconds = &seconds
	}
	if tests := strings.Fields(results["tests"]); len(tests) == 4 && sha256Hex.MatchString(tests[1]) {
		expected, e1 := strconv.Atoi(tests[0])
		ran, e2 := strconv.Atoi(tests[2])
		skipped, e3 := strconv.Atoi(tests[3])
		if e1 == nil && e2 == nil && e3 == nil && expected >= 0 && ran >= 0 && skipped >= 0 {
			row.TestsExpected, row.TestsRan, row.TestsSkipped, row.TestsListSHA = &expected, &ran, &skipped, &tests[1]
		}
	}
	if list := results["failed-tests"]; list != "" {
		for _, name := range strings.Split(list, ",") {
			if name == "…" {
				row.FailedTestsCut = true
			} else if name != "" {
				row.FailedTests = append(row.FailedTests, name)
			}
		}
	}
	var first *time.Time
	for _, step := range tr.Status.Steps {
		t := step.Terminated
		if t == nil || t.StartedAt == nil {
			continue
		}
		if first == nil || t.StartedAt.Before(*first) {
			first = t.StartedAt
		}
		if step.Name == "fetch" && t.FinishedAt != nil {
			row.FetchSeconds = seconds(*t.StartedAt, *t.FinishedAt)
		}
	}
	if first != nil && tr.Metadata.CreationTimestamp != nil {
		row.QueuedSeconds = seconds(*tr.Metadata.CreationTimestamp, *first)
	}
	// A pass is a pass of the tests it was to run: one that reports a count
	// which does not add up judged something else, and is not taken as one.
	if row.Class == store.UnitRunPassed && row.TestsExpected != nil && *row.TestsRan+*row.TestsSkipped != *row.TestsExpected {
		row.Class = store.UnitRunInfra
		row.Message = cut(fmt.Sprintf("the run passed, but ran %d and skipped %d of %d tests", *row.TestsRan, *row.TestsSkipped, *row.TestsExpected), messageMax)
	}
	return row, true, nil
}

func values(list []NameValue) map[string]string {
	out := make(map[string]string, len(list))
	for _, item := range list {
		out[item.Name] = item.Value
	}
	return out
}

func seconds(from, to time.Time) *int {
	n := int(to.Sub(from).Seconds())
	if n < 0 {
		return nil
	}
	return &n
}

func cut(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && s[len(s)-1]&0xC0 == 0x80 {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] >= 0xC0 {
		s = s[:len(s)-1]
	}
	return s
}
