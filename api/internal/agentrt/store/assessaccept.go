package store

import (
	"slices"
)

// An assessment's report and its record (TD-391; assess.go). The report is
// one JSON object; it is taken only whole: an option of the item, a
// confidence, two to four reasons each citing refs the spec gave (each ref is
// recorded as the record it names, so the Console links to it), and what the
// seat could not check. What the report says is advice, recorded on the
// item's stream and read by nothing that acts.

// approvalOptions are an approval's options as an assessment names them.
var approvalOptions = []AssessOption{{Key: "approve", Label: "Approve"}, {Key: "reject", Label: "Reject"}}

// ApprovalOptions are an approval's options for its assessment.
func ApprovalOptions() []AssessOption { return slices.Clone(approvalOptions) }
