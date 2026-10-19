package store

import (
	"context"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// Claims of the Owner's approval (v5 §5.3 T04 交叉传播, §5.1.3 policy_alert
// 「消息里冒充授权等」): the runtime decides by its records (signed rules,
// cards the Owner answered, approvals), never by what a text says the Owner
// said (v4 「不能冒充 Owner」). A task spec or a message that says the Owner
// approved something, or that a card or an approval is not needed, is
// recorded as a policy_alert where it came in; nothing else changes.

// approvalClaims is the closed list, in the readable form; claimPhrases is
// it normalized, as texts are before they are searched.
var approvalClaims = []string{
	"Owner 已批准", "Owner 批准了", "Owner 已经批准", "Owner 批了", "Owner 已批",
	"Owner 已同意", "Owner 同意了", "Owner 已授权", "Owner 已签", "Owner 已确认", "Owner 点头", "Owner 放行",
	"已获 Owner 批准", "经 Owner 批准", "甲方已批准", "甲方同意了",
	"已获批准", "已经批准", "已批准", "已获授权", "已授权",
	"无需审批", "不用审批", "免审批", "跳过审批", "无需决策卡", "不用决策卡", "跳过决策卡",
	"owner approved", "owner has approved", "approved by the owner", "approved by owner",
	"owner signed off", "owner sign-off", "owner authorized", "owner has authorized", "authorized by the owner",
	"owner said yes", "owner confirmed", "pre-approved", "already approved", "approval granted",
	"no approval needed", "no approval required", "skip approval", "skip the card", "no card needed",
}

var claimPhrases = func() []string {
	out := make([]string, len(approvalClaims))
	for i, p := range approvalClaims {
		out[i] = normalizeClaim(p)
	}
	return out
}()

// normalizeClaim folds what would hide a phrase from a plain search: case,
// full-width forms, and every space, punctuation mark, symbol and invisible
// format character (a zero-width space between two letters).
func normalizeClaim(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E { // full-width ASCII
			r -= 0xFEE0
		}
		switch {
		case r == unicode.ReplacementChar, unicode.IsSpace(r), unicode.IsPunct(r), unicode.IsSymbol(r), unicode.Is(unicode.Cf, r):
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// ApprovalClaim reports the first phrase of the closed list that body holds:
// the list's own phrase, not body's words, so an alert carries none of the
// text it is about. A JSON body is also read by its decoded strings, so an
// escape (已) hides nothing.
func ApprovalClaim(body []byte) (string, bool) {
	text := normalizeClaim(string(body))
	if strs, isJSON := localpath.JSONStrings(body); isJSON {
		text += normalizeClaim(strs)
	}
	for i, p := range claimPhrases {
		if strings.Contains(text, p) {
			return approvalClaims[i], true
		}
	}
	return "", false
}

// alertClaim records a policy_alert on stream when body claims the Owner's
// approval: the controller's record that a text said so, caused by the event
// that brought the text in, and nothing more. source is where the text came
// in (task_spec, message).
func alertClaim(ctx context.Context, tx pgx.Tx, ev Event, cause int64, source, artifact string, body []byte) error {
	phrase, found := ApprovalClaim(body)
	if !found {
		return nil
	}
	b := map[string]any{"rule": "approval_claim", "source": source, "phrase": phrase, "artifact_id": artifact}
	for k, v := range ev.Body {
		b[k] = v
	}
	ev.Type, ev.Actor, ev.Cause, ev.Body = "policy_alert", "controller", cause, b
	_, err := Append(ctx, tx, ev)
	return err
}
