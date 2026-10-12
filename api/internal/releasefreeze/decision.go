// Package releasefreeze decides whether a release is frozen from one LIST of
// freezes.release.bifrost.io. Frozen means the list contains a record. Clear
// means one complete, successful list returned zero records. Anything else is
// unreadable and is treated as frozen. The package holds no memory between calls.
package releasefreeze

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// VerdictClear is one complete LIST of zero records.
	VerdictClear = "clear"
	// VerdictFrozen is one complete LIST of one or more records.
	VerdictFrozen = "frozen"
	// VerdictUnreadable is any failed or incomplete read.
	VerdictUnreadable = "unreadable"

	// ListPath is the only request the gate sends: a cluster-scoped list with an empty query.
	ListPath = "/apis/release.bifrost.io/v1/freezes"

	apiVersion = "release.bifrost.io/v1"
	listKind   = "FreezeList"

	lineClear  = "未冻结：0 条记录"
	lineFrozen = "拒绝：发布已冻结（%d 条记录）；解冻由 Owner 删除记录：release.sh unfreeze"
	lineUnread = "拒绝：读不到冻结状态（%s），按冻结处理"
)

// ReasonTokens is the closed list of unreadable-reason tokens, shared with the
// Python gate. HTTP <code> and item <i> is malformed are patterns, not entries.
var ReasonTokens = []string{
	"timeout",
	"connection failed",
	"context canceled",
	"invalid utf-8",
	"bom",
	"not a JSON object",
	"duplicate key",
	"wrong apiVersion",
	"wrong kind",
	"metadata is not an object",
	"continue is not a string",
	"continue is not empty",
	"items is not a list",
	"missing token",
	"missing CA",
	"corrupt CA",
}

// Record is one freeze object as the gate shows it. Who, Reason and At are "?"
// when the field is missing or not a string. UID and Via are empty in that case.
type Record struct {
	Name   string
	UID    string
	Who    string
	Reason string
	At     string
	Via    string
}

// Decision is the result of one read. Lines[0] is the line both implementations compare.
type Decision struct {
	Verdict string
	Reason  string
	Records []Record
	Lines   []string
}

type jObj struct {
	keys []string
	m    map[string]any
}

func (o *jObj) dup() bool {
	seen := make(map[string]struct{}, len(o.keys))
	for _, k := range o.keys {
		if _, ok := seen[k]; ok {
			return true
		}
		seen[k] = struct{}{}
	}
	return false
}

// DecideHTTP applies the list rule to one HTTP status and body.
// Status other than 200 is unreadable and the body is not consulted.
func DecideHTTP(status int, body []byte) Decision {
	if status != 200 {
		return unread(fmt.Sprintf("HTTP %d", status))
	}
	if !utf8.Valid(body) {
		return unread("invalid utf-8")
	}
	if bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		return unread("bom")
	}
	val, err := parseOne(body)
	if err != nil {
		return unread("not a JSON object")
	}
	top, ok := val.(*jObj)
	if !ok {
		return unread("not a JSON object")
	}
	if watchedDup(top) {
		return unread("duplicate key")
	}
	av, ok := top.m["apiVersion"].(string)
	if !ok || av != apiVersion {
		return unread("wrong apiVersion")
	}
	kind, ok := top.m["kind"].(string)
	if !ok || kind != listKind {
		return unread("wrong kind")
	}
	meta, ok := top.m["metadata"].(*jObj)
	if !ok {
		return unread("metadata is not an object")
	}
	if raw, exists := meta.m["continue"]; exists {
		text, isString := raw.(string)
		if !isString {
			return unread("continue is not a string")
		}
		if text != "" {
			return unread("continue is not empty")
		}
	}
	items, ok := top.m["items"].([]any)
	if !ok {
		return unread("items is not a list")
	}
	recs := make([]Record, 0, len(items))
	for i, it := range items {
		rec, ok := itemRecord(it)
		if !ok {
			return unread(fmt.Sprintf("item %d is malformed", i))
		}
		recs = append(recs, rec)
	}
	if len(recs) == 0 {
		return Decision{Verdict: VerdictClear, Lines: []string{lineClear}}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	lines := make([]string, 0, len(recs)+1)
	lines = append(lines, fmt.Sprintf(lineFrozen, len(recs)))
	for _, rec := range recs {
		lines = append(lines, fmt.Sprintf("%s  who=%s  at=%s  reason=%s",
			rec.Name, flat(rec.Who), flat(rec.At), flat(rec.Reason)))
	}
	return Decision{Verdict: VerdictFrozen, Records: recs, Lines: lines}
}

// DecideTransport is a read that never got an HTTP status.
func DecideTransport(token string) Decision {
	return unread(token)
}

func unread(token string) Decision {
	return Decision{
		Verdict: VerdictUnreadable,
		Reason:  token,
		Lines:   []string{fmt.Sprintf(lineUnread, token)},
	}
}

func flat(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func watchedDup(top *jObj) bool {
	if top.dup() {
		return true
	}
	if meta, ok := top.m["metadata"].(*jObj); ok && meta.dup() {
		return true
	}
	items, ok := top.m["items"].([]any)
	if !ok {
		return false
	}
	for _, it := range items {
		obj, ok := it.(*jObj)
		if !ok {
			continue
		}
		if obj.dup() {
			return true
		}
		if meta, ok := obj.m["metadata"].(*jObj); ok && meta.dup() {
			return true
		}
	}
	return false
}

func itemRecord(it any) (Record, bool) {
	obj, ok := it.(*jObj)
	if !ok {
		return Record{}, false
	}
	meta, ok := obj.m["metadata"].(*jObj)
	if !ok {
		return Record{}, false
	}
	name, ok := meta.m["name"].(string)
	if !ok || name == "" {
		return Record{}, false
	}
	uid, _ := meta.m["uid"].(string)
	spec, _ := obj.m["spec"].(*jObj)
	via := ""
	if spec != nil {
		if s, ok := spec.m["via"].(string); ok {
			via = s
		}
	}
	return Record{
		Name:   name,
		UID:    uid,
		Who:    fieldOrQ(spec, "who"),
		Reason: fieldOrQ(spec, "reason"),
		At:     fieldOrQ(spec, "at"),
		Via:    via,
	}, true
}

func fieldOrQ(spec *jObj, key string) string {
	if spec == nil {
		return "?"
	}
	s, ok := spec.m[key].(string)
	if !ok {
		return "?"
	}
	return s
}

func parseOne(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	val, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, io.ErrUnexpectedEOF
	}
	return val, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return parseObject(dec)
		case '[':
			return parseArray(dec)
		default:
			return nil, io.ErrUnexpectedEOF
		}
	default:
		return tok, nil
	}
}

func parseObject(dec *json.Decoder) (*jObj, error) {
	obj := &jObj{m: map[string]any{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		val, err := parseValue(dec)
		if err != nil {
			return nil, err
		}
		obj.keys = append(obj.keys, key)
		obj.m[key] = val
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '}' {
		return nil, io.ErrUnexpectedEOF
	}
	return obj, nil
}

func parseArray(dec *json.Decoder) ([]any, error) {
	var items []any
	for dec.More() {
		val, err := parseValue(dec)
		if err != nil {
			return nil, err
		}
		items = append(items, val)
	}
	if items == nil {
		items = []any{}
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != ']' {
		return nil, io.ErrUnexpectedEOF
	}
	return items, nil
}
