package releasefreeze

import "fmt"

const (
	// VerdictClear is the only passing binary verdict: every transport row holds and the body is in L.
	VerdictClear = "clear"
	// VerdictFrozen is a refusal that could list records. It is not compared across implementations.
	VerdictFrozen = "frozen"
	// VerdictUnreadable is a refusal whose wording is for the reader.
	VerdictUnreadable = "unreadable"

	// ListPath is the only path the gate requests. The query is empty.
	ListPath = "/apis/release.bifrost.io/v1/freezes"

	lineClear  = "未冻结：0 条记录"
	lineFrozen = "拒绝：发布已冻结（%d 条记录）；解冻由 Owner 删除记录：release.sh unfreeze"
	lineUnread = "拒绝：读不到冻结状态（%s），按冻结处理"

	maxBody = 4096
)

// Record is one freeze object as the gate shows it. Who, Reason and At are "?"
// when the field is missing or not a string.
type Record struct {
	Name   string
	UID    string
	Who    string
	Reason string
	At     string
	Via    string
}

// Decision is one read. Only Verdict == VerdictClear is the binary pass.
type Decision struct {
	Verdict string
	Reason  string
	Records []Record
	Lines   []string
}

func bodyTooBig(n int) bool { return n > maxBody }

func statusIsList(status int) bool {
	if status == 404 {
		return false // 404 is not a list
	}
	if status == 403 {
		return false // 403 is not a list
	}
	if status == 500 {
		return false // 500 is not a list
	}
	return status == 200
}

// DecideHTTP is the body rule. Clear only when the status is a list status, the
// body is within the size cap, and the bytes are a member of L.
func DecideHTTP(status int, body []byte) Decision {
	if statusIsList(status) && !bodyTooBig(len(body)) && MemberOfL(body) {
		return Decision{Verdict: VerdictClear, Lines: []string{lineClear}}
	}
	return displayOnly(status, body)
}

// DecideTransport is a read that never got a complete response.
func DecideTransport(token string) Decision {
	return notClear(token)
}

func notClear(token string) Decision {
	return Decision{
		Verdict: VerdictUnreadable,
		Reason:  token,
		Lines:   []string{fmt.Sprintf(lineUnread, token)},
	}
}

// failClosed is the recoverable-failure answer. It is not clear.
func failClosed(token string) Decision {
	return notClear(token)
}
