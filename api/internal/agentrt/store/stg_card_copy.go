package store

import (
	"fmt"
	"strings"
	"time"
)

func cardChoiceLabel(kind, choice string) string {
	if kind == CardSTGBlocked && choice == "ack" {
		return "I will release STG manually"
	}
	if label := map[string]string{
		"retry_release":       "Allow up to three more automatic STG release attempts",
		"do_not_release":      "Do not release these changes to STG",
		"accept_unreconciled": "Accept the unreconciled execution risk and close quarantine",
	}[choice]; label != "" {
		return label
	}
	return cardChoiceLabels[choice]
}

func stgCardTitle(kind string) string {
	return map[string]string{CardSTGFailed: "STG release attempts exhausted", CardSTGBlocked: "STG release blocked", CardSTGIneligible: "Automatic STG release ineligible"}[kind]
}

func stgCardBody(repo string, tr CardTrigger) string {
	unknown := func(s string) string {
		if s == "" {
			return "Unknown"
		}
		return s
	}
	count := "Unknown"
	if tr.Members != nil {
		count = fmt.Sprint(len(tr.Members))
	}
	at := "Unknown"
	if !tr.At.IsZero() {
		at = tr.At.UTC().Format(time.RFC3339)
	}
	identity := "Block " + unknown(tr.Block)
	if tr.Batch != "" {
		identity = "Batch " + tr.Batch
	}
	lines := []string{fmt.Sprintf("Repository %s. %s. Since %s. Reason: %s. Members: %s. Target P: %s. Baseline P: %s.", repo, identity, at, unknown(tr.Why), count, unknown(tr.P), unknown(tr.BaseP))}
	switch tr.Kind {
	case CardSTGFailed:
		lines = append(lines, "Choosing more attempts records a new budget of up to three automatic STG attempts for each still-exhausted member and closes this exhaustion interval. Already delivered or disposed members are excluded. Choosing not to release withholds the remaining members and closes this interval. No release starts from this answer; the signed rule, live switch, current inputs and every final release check still apply.")
	case CardSTGBlocked:
		if tr.Sent != nil {
			lines = append(lines, stgSentBlockLines(tr)...)
		}
		if line := stgCIScopeLine(tr); line != "" {
			lines = append(lines, line)
		}
		lines = append(lines, "Noting that you will release STG manually leaves this block in place and authorizes no release. Remind later asks again in 24 hours if it still applies.")
		if tr.Batch != "" {
			lines = append(lines, "Accepting unreconciled risk closes this quarantine and releases its execution slot. It does not claim the run was reconciled or delivered, refund attempts, or start a release.")
		}
	case CardSTGIneligible:
		lines = append(lines, "These changes touch a hard floor and are withheld from automatic STG release. Acknowledging only records that you saw this notice. An answer cannot override the hard floor. Remind later asks again in 24 hours if it still applies.")
	}
	lines = append(lines, "An answer is not an approval. Approval effect: this card creates no approval and approves no deployment. Checks not performed by this card: independent review, fresh CI, deployment verification and STG health checks. Their current results are Unknown here; release evidence is recorded separately.")
	return strings.Join(lines, "\n\n")
}

// stgSentBlockLines explain a CI refusal found after B5 had sent P: STG
// already runs P, nothing is rolled back, and what lifts the block.
func stgSentBlockLines(tr CardTrigger) []string {
	sent := StgRecord(tr.Sent)
	unknown := func(s string) string {
		if s == "" {
			return "Unknown"
		}
		return s
	}
	verdict := "ended red"
	if tr.Why == "ci_results" {
		verdict = "ended with results that cannot be used"
	}
	return []string{
		fmt.Sprintf("P is already on STG: B5 sent it in batch %s, try %d, run %s, on an earlier green platform CI of the same commit. The platform CI run %s of P, which had no verdict when B5 sent it (still running, or not yet created), %s afterwards. STG keeps running P; nothing was rolled back.", unknown(sent.String("batch")), sent.Int("try"), unknown(sent.String("run")), unknown(sent.String("ci_run")), verdict),
		"Automatic STG release stays stopped until P has a newer green platform CI that ran the full API tests: rerun the platform CI (bifrost-ci-platform) for revision P. Neither a green CI of a later commit nor a manual STG release lifts this block; you can still release STG by hand meanwhile.",
	}
}

// stgCIScopeLine says, for a CI block, which CI scope decided P and how old
// the last full green was when B5 blocked: an incremental green stands for P
// only while that full green is within the cap.
func stgCIScopeLine(tr CardTrigger) string {
	if tr.CIScope == nil {
		return ""
	}
	c := StgRecord(tr.CIScope)
	scope := c.String("scope")
	if scope == "" {
		scope = "none yet"
	}
	line := fmt.Sprintf("Platform CI scope of P: %s.", scope)
	if c.Int("stale") > 0 {
		line += fmt.Sprintf(" %d incremental green CI run(s) of P set aside.", c.Int("stale"))
	}
	if c.Int("last_full_age_h") < 0 || c.String("last_full_run") == "" {
		return line + fmt.Sprintf(" No full CI green found; an incremental green counts only while the last full green is at most %d h old (cap %d h).", c.Int("cap_h"), c.Int("cap_h"))
	}
	return line + fmt.Sprintf(" Last full CI green %d h ago (run %s, commit %s), cap %d h.", c.Int("last_full_age_h"), c.String("last_full_run"), c.String("last_full_commit"), c.Int("cap_h"))
}

func stgCardNextStep(v LandingCardView) string {
	if v.Outcome == nil {
		if v.ClosedWhy == "resolved" {
			return "Resolved: the STG trigger no longer applies. No release was authorized by this card."
		}
		if v.Closed {
			return "This STG card ended (" + v.ClosedWhy + "). A new card is asked if the trigger still applies."
		}
		return "Waiting for your answer or its durable receipt. An answer is not an approval; no deployment starts from this card."
	}
	switch v.Outcome["outcome"] {
	case "applied":
		switch v.Outcome["why"] {
		case "release_granted":
			return "A new three-attempt budget was recorded. Automatic STG release still requires the signed rule, live switch and all release checks."
		case "release_withheld":
			return "The remaining exhausted members are withheld from STG release. No deployment was approved."
		case "quarantine_closed":
			return "Quarantine closed with unreconciled execution risk accepted. This is not delivery evidence and does not refund attempts."
		}
	case "acknowledged":
		return "Noted. The block or hard floor remains; no release was authorized."
	case "held":
		return "Held: asked again after " + valueString(v.Outcome["remind_at"]) + " if the STG trigger still applies."
	case "moot":
		return "Not carried out: the STG trigger or its remaining members had moved on. No new budget or disposition was recorded."
	case "refused":
		return "Not carried out (" + valueString(v.Outcome["why"]) + "). No release was authorized."
	}
	return "Unknown"
}
