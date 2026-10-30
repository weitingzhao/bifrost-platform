package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

func isStgCard(kind string) bool {
	return slices.Contains([]string{CardSTGFailed, CardSTGBlocked, CardSTGIneligible}, kind)
}

// Family is optional only for historical B3 records. STG has exactly one
// block/batch identity and cannot impersonate a root or repository stop.
func badCardFamily(m map[string]any) string {
	for _, key := range []string{"family", "block", "batch"} {
		if v, ok := m[key]; ok {
			if _, ok := v.(string); !ok {
				return "card " + key + " is not a string"
			}
		}
	}
	family, kind := valueString(m["family"]), valueString(m["kind"])
	if family != "" && family != "stg" || isStgCard(kind) != (family == "stg") {
		return "card family does not match its kind"
	}
	if family != "stg" {
		if _, ok := m["block"]; ok {
			return "landing card cannot name an STG block"
		}
		if _, ok := m["batch"]; ok {
			return "landing card cannot name an STG batch"
		}
		return ""
	}
	for _, k := range []string{"root", "op", "stop_generation"} {
		if _, ok := m[k]; ok {
			return "STG card cannot name a root, operation or stop generation"
		}
	}
	block, batch := valueString(m["block"]), valueString(m["batch"])
	if (block == "") == (batch == "") {
		return "STG card needs exactly one block or batch"
	}
	if block != "" && !stgHex16.MatchString(block) {
		return "STG card block is invalid"
	}
	if batch != "" && (stgBatchNumber(batch) == 0 || kind != CardSTGBlocked) {
		return "only an STG quarantine card names a batch"
	}
	return ""
}

func badStgCardOptions(m map[string]any, choices []string) string {
	want := cardChoices[valueString(m["kind"])][0]
	if valueString(m["batch"]) != "" {
		want = cardChoices[CardSTGBlocked][1]
	}
	if !slices.Equal(choices, want) {
		return "STG card choices do not match its block or quarantine"
	}
	return ""
}

func badStgCardOutcome(m map[string]any) string {
	kind, choice, outcome := valueString(m["kind"]), valueString(m["choice"]), valueString(m["outcome"])
	if v, exists := m["owner_answer"]; exists {
		if _, ok := v.(bool); !ok {
			return "STG owner_answer is not a boolean"
		}
	}
	if seq, ok := cardNumber(m["trigger_seq"]); !ok || seq < 1 {
		return "STG card outcome needs trigger_seq"
	}
	if why := badCardOutcomeChoice(m); why != "" {
		return why
	}
	if outcome == "applied" {
		if !stgOutcomeOwner(m) {
			return "STG applied answer requires owner"
		}
		if kind == CardSTGFailed && slices.Contains([]string{"retry_release", "do_not_release"}, choice) {
			if !stgMembers(m["members"], false, false) {
				return "STG budget answer needs its exact remaining members"
			}
		} else if kind != CardSTGBlocked || valueString(m["batch"]) == "" || choice != "accept_unreconciled" {
			return "STG applied choice does not match its kind"
		} else if _, ok := m["members"]; ok {
			return "quarantine acceptance binds its batch, not a budget member set"
		}
	} else if _, ok := m["members"]; ok {
		return "only applied STG budget answers name members"
	}
	if choice == "accept_unreconciled" && valueString(m["batch"]) == "" && outcome != "refused" {
		return "risk acceptance requires quarantine"
	}
	return ""
}

// New controller receipts carry the trusted adapter's authorization result
// and retain the actual login in by. The old canonical owner identity is
// accepted only when replaying a receipt without the new field; a fresh
// answer always records it, including false for a caller named owner.
func stgOutcomeOwner(m map[string]any) bool {
	if v, exists := m["owner_answer"]; exists {
		owner, _ := v.(bool)
		return owner && valueString(m["by"]) != ""
	}
	return m["by"] == "owner"
}

// Append rechecks the concrete member set, so a hand-built outcome cannot
// smuggle another block's members through the otherwise valid binding.
func checkStgCardOutcome(ctx context.Context, tx pgx.Tx, c CardRecord, m map[string]any) error {
	enabled, err := stgCardPolicy(ctx, tx)
	if err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("STG card handling is disabled by the rule")
	}
	if m["outcome"] != "applied" {
		return nil
	}
	s, err := StgReleaseState(ctx, tx)
	if err != nil {
		return err
	}
	tr, ok := matchingStgTrigger(s, c)
	if !ok {
		return fmt.Errorf("STG applied answer requires its current trigger")
	}
	if c.Kind == CardSTGFailed && !stgSame(m["members"], stgMemberValues(tr.Members)) {
		return fmt.Errorf("STG answer members are not the remaining exhausted set")
	}
	return nil
}
