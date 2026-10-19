package exam

import (
	"encoding/json"
	"fmt"
	"time"
)

// Grade grades answer to item id rendered at t0: every rule's verdict, in the
// set's order.
func (s *Set) Grade(id, answer string, t0 time.Time) ([]Result, error) {
	it := s.items[id]
	if it == nil {
		return nil, fmt.Errorf("exam: no item %s", id)
	}
	rec, err := s.records(it, t0)
	if err != nil {
		return nil, err
	}
	input, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	g := grading{t0: t0, input: string(input)}
	answer = asciiFold.Replace(answer)
	out := make([]Result, 0, len(it.Rules))
	for _, r := range it.Rules {
		ok, why := r.eval(answer, g)
		out = append(out, Result{Rule: r.ID, Hard: r.IsHard(), OK: ok, Why: why})
	}
	return out, nil
}

// Passed says whether a run passed: every hard rule holds.
func Passed(rs []Result) bool {
	for _, r := range rs {
		if r.Hard && !r.OK {
			return false
		}
	}
	return true
}
