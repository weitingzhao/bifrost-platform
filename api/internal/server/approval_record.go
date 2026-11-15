package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// approvedRecord is the approval an action executes under, as read back.
type approvedRecord struct {
	ID         string
	Number     int64
	Action     string
	ParamsHash string
	ApprovedAt time.Time
}

// grantApproval reads the approval this execution is for back from the
// approval service and checks it is what may write a grant (§5.6 item 4,
// §12.2 item 5): the context alone proves nothing, since any code in this
// process can build it (actions.WithCreateAttempt is exported).
func (s *Server) grantApproval(ctx context.Context, action string, params map[string]any) (approvedRecord, error) {
	att, ok := actions.CreateAttemptFrom(ctx)
	if !ok {
		return approvedRecord{}, errors.New(action + " runs only as an approval's execution")
	}
	if s.approvals == nil {
		return approvedRecord{}, errors.New("no approval service to read the approval back from")
	}
	rec, found, err := s.approvals.Lookup(att.ApprovalID)
	if err != nil {
		return approvedRecord{}, err
	}
	hash, err := actions.ParamsHash(params)
	if err != nil {
		return approvedRecord{}, err
	}
	ctxHash, _ := actions.ParamsHashFrom(ctx)
	if !found {
		return approvedRecord{}, fmt.Errorf("approval %s does not exist", att.ApprovalID)
	}
	if err := checkGrantRecord(rec, action, hash, ctxHash, approvalhome.IsProd()); err != nil {
		return approvedRecord{}, err
	}
	return approvedRecord{ID: rec.ID, Number: int64(rec.Number), Action: rec.Action, ParamsHash: rec.ParamsHash, ApprovedAt: rec.DecidedAt}, nil
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

// approvalOpenKey is the one-open-request rule of every action that has one.
//
// Two more subjects have one (2026-11-15; origin: card #575/#576, the same
// command asked twice by one session left two cards pending, and #562/#564/
// #566 for releases): an execution card is the one command it carries, and a
// release card is the pipeline it runs. A requester's newer card of the same
// subject replaces its own unrun older one (approvals/withdraw.go); another
// requester's open card of the same subject still blocks (409).
func approvalOpenKey(action string, params map[string]any) string {
	switch action {
	case "owner_run_command":
		command, _ := params["command"].(string)
		if strings.TrimSpace(command) == "" {
			return ""
		}
		sum := sha256.Sum256([]byte(command))
		return "exec:" + hex.EncodeToString(sum[:])
	case "start_pipeline_run":
		name, _ := params["name"].(string)
		if strings.TrimSpace(name) == "" {
			return ""
		}
		return "pipeline:" + strings.TrimSpace(name)
	}
	return ruleApproveOpenKey(action, params)
}
