package questions

import (
	"sort"
	"time"
)

// Observations returns every retained question without sweeping or writing.
// Absence from this bounded history does not imply a state transition.
func (s *Service) Observations() ([]Question, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Question, 0, len(doc.Questions))
	for _, q := range doc.Questions {
		out = append(out, q.public(""))
	}
	return out, nil
}

// HumanAnswerTimes is when each retained question was answered by a person:
// the answer's time, for every answer that is not a rule's (RuleChannel),
// newest first, without sweeping or writing. Origin: the foundation line
// (2026-11-21) clearing the domain wall's debt: delivery's release context
// needs only these times, and a port of this shape lets it stop importing
// this package.
func (s *Service) HumanAnswerTimes() ([]time.Time, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	var out []time.Time
	for _, q := range doc.Questions {
		if q.Answer == nil || q.Answer.Channel == RuleChannel || q.Answer.At.IsZero() {
			continue
		}
		out = append(out, q.Answer.At)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].After(out[j]) })
	return out, nil
}
