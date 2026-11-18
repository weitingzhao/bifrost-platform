package eventlog

import "regexp"

// legacyImportedType is the record of data brought in from elsewhere: the head
// of an import (source names what it came from, count how many it holds) or,
// on a recorder log's stream, one line of the log (source recorder; the line's
// seq, its original digest, the body as kept, masked when its local paths
// were). Several domains write it, so the log itself declares it.
const legacyImportedType = "legacy_imported"

func init() {
	Declare(Vocabulary{
		Types: []string{legacyImportedType},
		Bodies: map[string][]Field{
			legacyImportedType: {{Name: "source", Kind: Str}, {Name: "count", Kind: Int, Optional: true},
				{Name: "work_id", Kind: Str, Optional: true}, {Name: "seq", Kind: Int, Optional: true},
				{Name: "digest", Kind: Str, Optional: true}, {Name: "record", Kind: Str, Optional: true},
				{Name: "masked", Kind: Bool, Optional: true}},
		},
		Values: map[string]map[string]Value{
			legacyImportedType: {"digest": Shaped("a fingerprint (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))},
		},
	})
}
