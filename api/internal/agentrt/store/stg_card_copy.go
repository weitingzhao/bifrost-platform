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
