package releases

import "testing"

func TestPodDigestsZeroReplicasFullyScaledDown(t *testing.T) {
	t.Parallel()
	if got, err := settledWorld().read(t); err != nil || len(got) != 2 {
		t.Fatalf("positive control: digests=%v err=%v", got, err)
	}
	w := settledWorld()
	zero := int32(0)
	w.dep.Spec.Replicas = &zero
	w.dep.Status.Replicas = 0
	w.dep.Status.UpdatedReplicas = 0
	w.dep.Status.ReadyReplicas = 0
	w.dep.Status.AvailableReplicas = 0
	w.pods = nil
	if got, err := w.read(t); err == nil {
		t.Fatalf("fully scaled-down deployment accepted as settled: digests=%v", got)
	}
}
