package store

import (
	"fmt"
	"testing"
)

func TestReview760RevertedBeforeFirstBatchCanClose(t *testing.T) {
	t.Parallel()
	for _, admitted := range []bool{false, true} {
		t.Run(fmt.Sprintf("admitted=%v", admitted), func(t *testing.T) {
			t.Parallel()
			h := newStgHarness(t)
			if admitted {
				h = stgReady(t, stgTestMember())
			}
			m := stgMember(StgRecord(stgTestMember()))
			h.landed(m.Op, m.Root, m.R)
			h.landed("land:reversal:1", stgTestRoot2, stgSHA("f"))
			h.raw(map[string]any{"text": "authorized reversal", "landing": stgLegacyRecord(m.Op, "reverted", "R", m.R, "by", stgSHA("f"))})
			if admitted {
				h.add(memberRecord(stgTestMember(), "disposed", int64(len(h.entries))))
			}
			s := h.state()
			if !s.Legacy.Operations[m.Op].Reverted || !admitted && len(s.Members) != 0 {
				t.Fatal("fixture did not reach authorized reversal")
			}
			for _, candidate := range StgCoverageCandidates(s) {
				if candidate == m {
					t.Fatal("reversed root unexpectedly eligible for coverage")
				}
			}
			c := classifyStgRoot(LandingRootClass{Root: m.Root, Op: m.Op, Class: RootUnresolved}, s, m, false, false, true,
				mainObservation{validity: "reverted", revertedBy: stgSHA("f"), readAt: h.at}, true)
			if c.Class != RootDisposed {
				t.Fatalf("authorized reversal before admission never settles: class=%s reason=%s phase=%s", c.Class, c.Reason, c.STG.Phase)
			}
		})
	}
}

func TestStgB4ReversalRequiresLandedProof(t *testing.T) {
	t.Parallel()
	for _, legal := range []bool{false, true} {
		for _, admitted := range []bool{false, true} {
			t.Run(fmt.Sprintf("legal=%t/admitted=%t", legal, admitted), func(t *testing.T) {
				t.Parallel()
				h := newStgHarness(t)
				if admitted {
					h = stgReady(t, stgTestMember())
				}
				m := stgMember(StgRecord(stgTestMember()))
				h.landed(m.Op, m.Root, m.R)
				before := stgSnapshotKey(h.state(), m)
				if legal {
					h.landed("land:reversal:1", stgTestRoot2, stgSHA("f"))
				}
				h.raw(map[string]any{"landing": stgLegacyRecord(m.Op, "reverted", "R", m.R, "by", stgSHA("f"))})
				ref := int64(len(h.entries))
				if legal && admitted {
					pending := classifyStgRoot(LandingRootClass{Root: m.Root, Op: m.Op, Class: RootUnresolved}, h.state(), m, false, false, true, mainObservation{}, false)
					if pending.Class == RootDisposed || pending.key != before {
						t.Fatal("admitted root bypassed normal member settlement")
					}
					h.add(memberRecord(stgTestMember(), "disposed", ref))
				}
				s := h.state()
				c := classifyStgRoot(LandingRootClass{Root: m.Root, Op: m.Op, Class: RootUnresolved}, s, m, false, false, true, mainObservation{validity: "reverted", revertedBy: stgSHA("f"), readAt: h.at}, true)
				if (c.Class == RootDisposed) != legal || (c.STG.Phase == "disposed") != legal {
					t.Fatal("reversal authority ignored", c)
				}
				if (c.key != before) != legal {
					t.Fatal("snapshot must track only authorized reversal identity")
				}
				key := c.key
				h.effective(stgTestMember(), stgSHA("f"), "unknown")
				if stgSnapshotKey(h.state(), m) != key {
					t.Fatal("fresh read changed reversal identity")
				}
				wrong := m
				wrong.Root = stgTestRoot2
				other := classifyStgRoot(LandingRootClass{Root: wrong.Root, Op: wrong.Op, Class: RootUnresolved}, s, wrong, false, false, true, mainObservation{}, false)
				if other.Class == RootDisposed {
					t.Fatal("borrowed another root's proof")
				}
			})
		}
	}
}
