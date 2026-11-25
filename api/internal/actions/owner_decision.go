package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// owner_decision is the Owner's decision kept as a record (ISSUE-105): what
// he decided, under which law, from which message, and exactly which changes
// it covers (items: wall numbers, action ids, commits). The scope is what he
// approves on the card, not what a line later says he meant, so a line acting
// on it cites the card number instead of a relay. The platform runs nothing:
// the approved card is the record; GET /api/v1/approvals?item=<item> finds
// the executed decisions that name an item.

// OwnerDecisionAction is the catalog id.
const OwnerDecisionAction = "owner_decision"

const (
	decisionMaxItems   = 200
	decisionMaxItemLen = 120
	decisionMaxText    = 2000
)

// DecisionItems reads params["items"] as a list of strings.
func DecisionItems(params map[string]any) []string {
	switch v := params["items"].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			s, _ := x.(string)
			out = append(out, s)
		}
		return out
	}
	return nil
}

// CheckOwnerDecision is the create check: the scope must be a list of named
// items, and the law, the source and the text must be there.
func CheckOwnerDecision(_ context.Context, params map[string]any) error {
	items := DecisionItems(params)
	if len(items) == 0 {
		return errors.New("owner_decision: items is required: the list of changes this decision covers (wall numbers, action ids, commits)")
	}
	if len(items) > decisionMaxItems {
		return fmt.Errorf("owner_decision: at most %d items on one card", decisionMaxItems)
	}
	seen := map[string]bool{}
	for _, it := range items {
		t := strings.TrimSpace(it)
		if t == "" || t != it || len(t) > decisionMaxItemLen {
			return fmt.Errorf("owner_decision: item %q must be a non-empty trimmed string of at most %d bytes", it, decisionMaxItemLen)
		}
		if seen[t] {
			return fmt.Errorf("owner_decision: item %q is listed twice", t)
		}
		seen[t] = true
	}
	for _, k := range []string{"law", "source", "text"} {
		if strings.TrimSpace(str(params[k])) == "" {
			return fmt.Errorf("owner_decision: %s is required", k)
		}
	}
	if len(str(params["text"])) > decisionMaxText {
		return fmt.Errorf("owner_decision: text is at most %d bytes", decisionMaxText)
	}
	return nil
}

func ownerDecisionSummary(params map[string]any) string {
	return fmt.Sprintf("Owner decision under %s: %d item(s)", strings.TrimSpace(str(params["law"])), len(DecisionItems(params)))
}

// ExecuteOwnerDecision records the approval; nothing else happens.
func ExecuteOwnerDecision(_ context.Context, params map[string]any) (any, error) {
	return map[string]any{
		"recorded": true, "executed_by_platform": false,
		"items": DecisionItems(params), "law": str(params["law"]), "source": str(params["source"]),
	}, nil
}
