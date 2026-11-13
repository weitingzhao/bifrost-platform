package store

import "regexp"

// ExamsStream is the stream the executor exams wrote to (papers issued,
// answers taken, scores) until the exams were removed on 2026-11-13: PROD's
// last exam run was 2026-10-20 and its routes answered 503. The records stay
// in the database, so the artifact reads still keep them from executors
// (reads.go, artifactsearch.go): a paper's artifacts hold its questions.
const ExamsStream = "capability_exams"

// approvalIDRE is the shape of an approval's id.
var approvalIDRE = regexp.MustCompile(`^appr_[0-9a-f]{16}$`)
