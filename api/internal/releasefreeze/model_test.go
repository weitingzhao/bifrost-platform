package releasefreeze

import (
	"testing"
	"time"
)

// Counts from the exhaustive search. A change to the alphabet is a diff here.
const (
	wantStates      = 963
	wantTransitions = 134984
)

func TestExhaustive(t *testing.T) {
	alpha := alphabet()
	if len(alpha) != 188 {
		t.Fatalf("alphabet %d, want 188", len(alpha))
	}
	t.Logf("alphabet identities=5 names=2 classes=7 events=%d", len(alpha))

	start := time.Now()
	s1, t1, v1 := explore(correctRules())
	elapsed := time.Since(start)
	s2, t2, v2 := explore(correctRules())
	if v1 != "" {
		t.Fatalf("violation %s", v1)
	}
	if v2 != "" {
		t.Fatalf("second run violation %s", v2)
	}
	if s1 != s2 || t1 != t2 {
		t.Fatalf("runs differ: states %d/%d transitions %d/%d", s1, s2, t1, t2)
	}
	t.Logf("states %d", s1)
	t.Logf("transitions %d", t1)
	t.Logf("exhaustive seconds %.3f", elapsed.Seconds())
	if elapsed > 20*time.Second {
		t.Fatalf("exhaustive search took %s, budget is 20s", elapsed)
	}
	if s1 != wantStates || t1 != wantTransitions {
		t.Fatalf("counts states=%d transitions=%d, constants states=%d transitions=%d", s1, t1, wantStates, wantTransitions)
	}
}

func TestNegativeOwnerRef(t *testing.T) {
	r := correctRules()
	r.refuseOwnerRef = false
	path := search(r, func(path []event, w world) bool {
		phase := 0
		for _, e := range path {
			switch phase {
			case 0:
				if e.kind == evCreate && e.class == classOwnerRef {
					phase = 1
				}
			case 1:
				if e.kind == evSweep {
					phase = 2
				}
			case 2:
				if e.kind == evRead && e.gate == gateFresh {
					phase = 3
				}
			}
		}
		if phase != 3 || anyPresent(w) {
			return false
		}
		d := DecideHTTP(200, listBody(w))
		return d.Verdict == VerdictClear && !allDeletedByO(w)
	})
	if path == nil {
		t.Fatal("negative control did not find create ownerRef → sweep → fresh clear")
	}
	t.Logf("negative ownerRef %s", formatPath(path))
}

func TestNegativeCreateGuard(t *testing.T) {
	r := correctRules()
	r.createNeedsTombstone = false
	path := search(r, func(path []event, w world) bool {
		if len(path) == 0 {
			return false
		}
		last := path[len(path)-1]
		if last.kind != evRead || last.gate != gateOld {
			return false
		}
		if !w.create || w.old != oldFalse || !oldAllows(w) || !anyPresent(w) {
			return false
		}
		created := false
		enabled := false
		for _, e := range path {
			if e.kind == evEnableCreate {
				enabled = true
			}
			if enabled && e.kind == evCreate && e.class == classCompliant {
				created = true
			}
		}
		return created
	})
	if path == nil {
		t.Fatal("negative control did not find the old program allowing after create was enabled")
	}
	t.Logf("negative create-guard %s", formatPath(path))
}

func TestNegativeAdmission(t *testing.T) {
	r := correctRules()
	r.admission = false
	path := search(r, func(path []event, w world) bool {
		if len(path) == 0 {
			return false
		}
		last := path[len(path)-1]
		if last.kind != evRead || last.gate != gateFresh {
			return false
		}
		deletedByW := false
		for _, e := range path {
			if e.kind == evDelete && e.who == idW && e.prop == propPlain && e.uidMode != uidStale {
				deletedByW = true
			}
		}
		if !deletedByW || anyPresent(w) {
			return false
		}
		d := DecideHTTP(200, listBody(w))
		return d.Verdict == VerdictClear && !allDeletedByO(w)
	})
	if path == nil {
		t.Fatal("negative control did not find W's delete violating S")
	}
	t.Logf("negative admission %s", formatPath(path))
}
