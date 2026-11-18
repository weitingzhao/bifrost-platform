package unitexec

import (
	"encoding/json"
	"strings"
)

// PlanOfSelector reads the two selectors a cache of the unit TaskRuns can
// answer: every one of them (the plan label alone, or nothing beyond it) gives
// "", one plan's gives that plan. Anything else is for the cluster to answer.
func PlanOfSelector(label, selector string) (string, bool) {
	if label != LabelPlan {
		return "", false
	}
	if selector == "" || selector == label {
		return "", true
	}
	plan, ok := strings.CutPrefix(selector, label+"=")
	if !ok || plan == "" || strings.ContainsAny(plan, ",!() ") {
		return "", false
	}
	return plan, true
}

// CompareKey is "<name>\x00<the TaskRun as the unit loops decode it>", for
// comparing two reads of one: what the evidence reads is what is compared, and
// a field added to TaskRun is compared from then on.
func CompareKey(name string, obj map[string]any) string {
	raw, err := json.Marshal(obj)
	var tr TaskRun
	if err == nil {
		err = json.Unmarshal(raw, &tr)
	}
	if err == nil {
		raw, err = json.Marshal(tr)
	}
	if err != nil {
		return name + "\x00undecodable: " + err.Error()
	}
	return name + "\x00" + string(raw)
}
