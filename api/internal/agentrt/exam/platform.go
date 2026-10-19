package exam

import (
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The set as the platform hands it out and grades it (store.ExamSet; the
// routes GET /agentrt/exams/{job} and POST /agentrt/exams, the exam's cut
// §6 甲): a paper renders each item at its own t0 for the paper's time, and
// an answer is graded at the t0 its item was rendered at.

// Platform is s as the platform's exam set; refused when a question is of a
// job, or under a class, the store keeps no exam score for.
func Platform(s *Set) (store.ExamSet, error) {
	for _, q := range s.Questions {
		if !store.ExamClass(q.Exam, q.Class) {
			return nil, fmt.Errorf("exam: question %s: job %q and class %q are not an exam the platform scores", q.ID, q.Exam, q.Class)
		}
	}
	return platform{s}, nil
}

type platform struct{ s *Set }

func (p platform) Digest() string { return p.s.SHA256 }

func (p platform) Items(job string) []store.ExamItem {
	var out []store.ExamItem
	for _, q := range p.s.Questions {
		if q.Exam != job {
			continue
		}
		for _, it := range q.Items {
			out = append(out, store.ExamItem{ID: it.ID, Class: q.Class})
		}
	}
	return out
}

func (p platform) Render(item string, now time.Time) (store.ExamSpec, error) {
	it := p.s.Item(item)
	if it == nil {
		return store.ExamSpec{}, fmt.Errorf("exam: no item %s", item)
	}
	t0 := T0(it, now)
	paper, err := p.s.Render(item, t0)
	if err != nil {
		return store.ExamSpec{}, err
	}
	return store.ExamSpec{T0: t0, Spec: paper.Spec, Repo: paper.Repo, BaseCommit: paper.BaseCommit}, nil
}

func (p platform) Grade(item, answer string, t0 time.Time) (store.ExamVerdict, error) {
	rs, err := p.s.Grade(item, answer, t0)
	if err != nil {
		return store.ExamVerdict{}, err
	}
	v := store.ExamVerdict{Passed: Passed(rs), Failed: []string{}, Flagged: []string{}}
	for _, r := range rs {
		switch {
		case r.OK:
		case r.Hard:
			v.Failed = append(v.Failed, r.Rule)
		default:
			v.Flagged = append(v.Flagged, r.Rule)
		}
	}
	return v, nil
}
