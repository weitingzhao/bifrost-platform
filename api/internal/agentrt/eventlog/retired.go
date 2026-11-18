package eventlog

import "strings"

// retiredNoteFields are the records of the landing lane, the B5 STG release
// and the platform's waves. Their writers were removed on 2026-11-14
// (LANE-BIZ-ALIGN, store cut 1); rows already written stay readable until
// the table clean-up.
var retiredNoteFields = []string{"landing", "stg_release", "stg_sample", "landing_answer", "landing_gate", "landing_grant_request",
	"landing_grant", "landing_grant_revoked", "landing_grant_use", "landing_grant_refused", "landing_ci_key", "landing_rule_clear",
	"landing_batch_grant", "landing_anomaly", "landing_main", "landing_stop", "landing_resume", "landing_card_prepared",
	"landing_card_ask_started", "landing_card", "landing_card_acked", "landing_card_closed", "landing_card_outcome",
	"landing_close_round", "landing_close", "landing_refusal", "landing_floor", "landing_repo", "landing_pusher", "landing_disposed",
	"landing_delivered_by", "landing_rework_seen", "landing_replaced", "landing_batch", "landing_batch_red",
	"wave_opened", "wave_appended", "wave_append_refused", "wave_closed", "wave_landing", "wave_red", "wave_landed",
	"wave_abandoned", "wave_tally"}

// retiredStreams are the prefixes of the streams those records lived on.
var retiredStreams = []string{"landing:", "landing-cards:", "stg-sample:", "wave:"}

// RetiredRecord refuses an event that would write one of them: the stream is
// retired, and nothing checks such a record any more. It only ever judges a
// new event; what is already stored is read as it always was. The runtime's
// vocabulary declares it among the checks asked of every event.
func RetiredRecord(ev Event) error {
	for _, prefix := range retiredStreams {
		if strings.HasPrefix(ev.Stream, prefix) {
			return Invalid("stream %s is retired: the landing lane, the B5 STG release and waves ended on 2026-11-14", ev.Stream)
		}
	}
	if ev.Type != "note" {
		return nil
	}
	for _, name := range retiredNoteFields {
		if _, ok := ev.Body[name]; ok {
			return Invalid("the %s record is retired: the landing lane, the B5 STG release and waves ended on 2026-11-14", name)
		}
	}
	return nil
}
