// Package approvalrules holds what the platform decides around an approval
// that the approval service does not decide itself: whether the approval an
// execution claims is one that may write a grant (GrantApproval), which open
// request a new one collides with (OpenKey), and when a request the signed
// release policy covers is run instead of answered (CoveredRequestRuns). All
// three moved unchanged out of api/internal/server (decoupling item 5).
package approvalrules

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/owner/approvalhome"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// ApprovedRecord is the approval an action executes under, as read back.
type ApprovedRecord struct {
	ID         string
	Number     int64
	Action     string
	ParamsHash string
	ApprovedAt time.Time
}

// GrantApproval reads the approval this execution is for back from the
// approval service and checks it is what may write a grant (§5.6 item 4,
// §12.2 item 5): the context alone proves nothing, since any code in this
// process can build it (actions.WithCreateAttempt is exported).
func GrantApproval(ctx context.Context, svc *approvals.Service, action string, params map[string]any) (ApprovedRecord, error) {
	att, ok := actions.CreateAttemptFrom(ctx)
	if !ok {
		return ApprovedRecord{}, errors.New(action + " runs only as an approval's execution")
	}
	if svc == nil {
		return ApprovedRecord{}, errors.New("no approval service to read the approval back from")
	}
	rec, found, err := svc.Lookup(att.ApprovalID)
	if err != nil {
		return ApprovedRecord{}, err
	}
	hash, err := actions.ParamsHash(params)
	if err != nil {
		return ApprovedRecord{}, err
	}
	ctxHash, _ := actions.ParamsHashFrom(ctx)
	if !found {
		return ApprovedRecord{}, fmt.Errorf("approval %s does not exist", att.ApprovalID)
	}
	if err := checkGrantRecord(rec, action, hash, ctxHash, approvalhome.IsProd()); err != nil {
		return ApprovedRecord{}, err
	}
	return ApprovedRecord{ID: rec.ID, Number: int64(rec.Number), Action: rec.Action, ParamsHash: rec.ParamsHash, ApprovedAt: rec.DecidedAt}, nil
}

// checkGrantRecord is the read-back record's check (§5.6 item 4, §12.2
// item 5): the action executed, tier D, running now, the parameters it
// was approved with (stored hash, the execution's hash and the executed
// parameters' hash all equal), not a drill, approved on the Console with
// its number confirmed, decided, and PROD's.
func checkGrantRecord(rec approvals.Approval, action, hash, ctxHash string, prod bool) error {
	switch {
	case rec.Action != action:
		return fmt.Errorf("approval %s is for %s, not %s", rec.ID, rec.Action, action)
	case rec.Tier != string(actions.TierD):
		return fmt.Errorf("approval %s is tier %s, not D", rec.ID, rec.Tier)
	case rec.Status != approvals.StatusRunning:
		return fmt.Errorf("approval %s is %s, not running", rec.ID, rec.Status)
	case hash == "" || rec.ParamsHash != hash || ctxHash != hash:
		return fmt.Errorf("approval %s's params_hash does not match the parameters executed", rec.ID)
	case rec.Drill:
		return fmt.Errorf("approval %s is a drill", rec.ID)
	case rec.Channel != "console":
		return fmt.Errorf("approval %s was approved on %q; a landing grant needs the Console", rec.ID, rec.Channel)
	case rec.Number < 1 || rec.ConfirmedNumber != rec.Number:
		return fmt.Errorf("approval %s was approved without its number confirmed", rec.ID)
	case rec.DecidedAt.IsZero():
		return fmt.Errorf("approval %s has no decision time", rec.ID)
	case !prod || rec.Params["env"] != "prod":
		return errors.New("a landing grant's approval is PROD's: " + approvalhome.Required)
	}
	return nil
}
