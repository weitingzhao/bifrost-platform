package delivery

import (
	"context"
	"fmt"
)

// Delivery's tests run as the server does, with an action runner bound. This
// stub stands in for the approvals domain's runner (actions): an approved
// attempt is a value in the context, and the run is named base-<id>-<n>.
// Origin: 2026-11-22, delivery stopped importing actions (the domain wall's
// row "delivery -> approvals"); the tests that had built attempts with
// actions.WithCreateAttempt use withTestAttempt instead.

type testAttempt struct {
	id string
	n  int
}

type testAttemptKey struct{}

const testNotRecordedHeader = "Test-Not-Recorded"

func withTestAttempt(ctx context.Context, id string, n int) context.Context {
	return context.WithValue(ctx, testAttemptKey{}, testAttempt{id: id, n: n})
}

func init() {
	BindActionRuns(ActionRuns{
		AttemptName: func(ctx context.Context, base string) (string, bool) {
			a, ok := ctx.Value(testAttemptKey{}).(testAttempt)
			if !ok {
				return "", false
			}
			return fmt.Sprintf("%s-%s-%d", base, a.id, a.n), true
		},
		NotRecordedHeader: testNotRecordedHeader,
	})
}
