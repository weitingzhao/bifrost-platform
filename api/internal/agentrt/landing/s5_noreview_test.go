package landing

import "testing"

// R5:5.7-9: an attempt accepted with no review (on submission, the landing
// CI its check) binds its acceptance alone: no S5_REVIEW_BINDING, no
// S5_REVIEW_MISSING. The binding still names the attempt, its result and
// its patch, and carries no review field.
func TestS5AcceptanceWithNoReview(t *testing.T) {
	f, _ := s5Ready(t)
	r := f.S5Review
	b := ReviewBinding{Repo: r.Repo, Task: r.Task, AcceptedAttempt: r.AcceptedAttempt, A: r.A, Base: r.Base,
		AcceptanceVerificationID: "v_accept", ImplementerAttempt: r.ImplementerAttempt, ImplementerExecutor: r.ImplementerExecutor,
		ImplementerProvider: r.ImplementerProvider, ReviewedPatchSHA256: r.ReviewedPatchSHA256, ReviewedStablePatchID: r.ReviewedStablePatchID, NoReview: true}
	if why := ValidateS5Review(f, b); why != "" {
		t.Fatal("the acceptance alone:", why)
	}
	f.S5Review = b
	if _, why := NewS5CIKey(f, "land:"+f.Task+":1", 1); why != "" {
		t.Fatal("its key:", why)
	}
	without := b
	without.NoReview = false
	if ValidateS5Review(f, without) == "" {
		t.Fatal("no review, not marked so, accepted")
	}
	for name, change := range map[string]func(*ReviewBinding){
		"another attempt":  func(b *ReviewBinding) { b.AcceptedAttempt = "a_other" },
		"another commit":   func(b *ReviewBinding) { b.A = M0 },
		"no acceptance":    func(b *ReviewBinding) { b.AcceptanceVerificationID = "" },
		"another patch":    func(b *ReviewBinding) { b.ReviewedPatchSHA256 = "" },
		"a review too":     func(b *ReviewBinding) { b.ReviewVerificationID = "v_review" },
		"no implementer":   func(b *ReviewBinding) { b.ImplementerProvider = "" },
		"reader's problem": func(b *ReviewBinding) { b.Problem = "S5_REVIEW_PROVIDER" },
	} {
		c := b
		change(&c)
		if ValidateS5Review(f, c) == "" {
			t.Errorf("%s accepted", name)
		}
	}
}
