// Package approvals stores approval records in one JSON state file.
// A release that changes this file's shape must not run two versions of the writer at the same time: stop the old writer and wait until it has finished before starting the new one. Unknown keys on the document, on each approval, inside execution, and inside each deliveries entry are written back as they were read. Keeping those fields does not make overlapping writers safe.
package approvals

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// file is the on-disk document. The statefile key is the path relative to
// PLATFORM_DATA_DIR, which is "approvals" (PROD: ConfigMap platform-state-approvals).
// extra keeps top-level JSON keys this binary does not know so a rewrite
// does not drop them.
type file struct {
	// LastNumber is the #n counter. It survives pruning and only grows.
	LastNumber int        `json:"last_number,omitempty"`
	Approvals  []Approval `json:"approvals"`
	extra      map[string]json.RawMessage
}

// maxDocBytes stays under the ConfigMap budget (k8sstate.MaxBytes, 900 KiB):
// past it the oldest terminal records are dropped rather than failing a write.
const maxDocBytes = 800 * 1024

type store struct {
	path string
}

func newStore(path string) *store {
	return &store{path: path}
}

func (s *store) read() (file, error) {
	if s.path == "" {
		return file{}, nil
	}
	data, err := statefile.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return file{}, nil
		}
		return file{}, err
	}
	return parse(data)
}

func parse(data []byte) (file, error) {
	var doc file
	if len(data) == 0 {
		return doc, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return file{}, err
	}
	if raw, ok := top["last_number"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &doc.LastNumber); err != nil {
			return file{}, err
		}
	}
	if raw, ok := top["approvals"]; ok && len(raw) > 0 && string(raw) != "null" {
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			return file{}, err
		}
		doc.Approvals = make([]Approval, 0, len(rows))
		for _, row := range rows {
			a, err := parseApproval(row)
			if err != nil {
				return file{}, err
			}
			doc.Approvals = append(doc.Approvals, a)
		}
	}
	extra := map[string]json.RawMessage{}
	for k, v := range top {
		if k == "last_number" || k == "approvals" {
			continue
		}
		extra[k] = append(json.RawMessage(nil), v...)
	}
	if len(extra) > 0 {
		doc.extra = extra
	}
	return doc, nil
}

func parseApproval(raw json.RawMessage) (Approval, error) {
	var a Approval
	if len(raw) == 0 || string(raw) == "null" {
		return a, nil
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return Approval{}, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Approval{}, err
	}
	extra := map[string]json.RawMessage{}
	for k, v := range obj {
		if approvalJSONKeys[k] {
			continue
		}
		extra[k] = append(json.RawMessage(nil), v...)
	}
	if len(extra) > 0 {
		a.extra = extra
	}
	if rawExec, ok := obj["execution"]; ok && a.Execution != nil {
		if err := attachUnknown(&a.Execution.extra, rawExec, executionJSONKeys); err != nil {
			return Approval{}, err
		}
	}
	if rawDels, ok := obj["deliveries"]; ok && len(a.Deliveries) > 0 {
		var rows []json.RawMessage
		if err := json.Unmarshal(rawDels, &rows); err != nil {
			return Approval{}, err
		}
		if len(rows) != len(a.Deliveries) {
			return Approval{}, errors.New("approvals: deliveries do not match")
		}
		for i := range rows {
			if err := attachUnknown(&a.Deliveries[i].extra, rows[i], deliveryJSONKeys); err != nil {
				return Approval{}, err
			}
		}
	}
	return a, nil
}

// attachUnknown keeps object keys this binary does not know, byte for byte.
func attachUnknown(dst *map[string]json.RawMessage, raw json.RawMessage, known map[string]bool) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	extra := map[string]json.RawMessage{}
	for k, v := range obj {
		if known[k] {
			continue
		}
		extra[k] = append(json.RawMessage(nil), v...)
	}
	if len(extra) > 0 {
		*dst = extra
	}
	return nil
}

var (
	approvalJSONKeys  = jsonFieldNames(Approval{})
	executionJSONKeys = jsonFieldNames(Execution{})
	deliveryJSONKeys  = jsonFieldNames(Delivery{})
)

func jsonFieldNames(v any) map[string]bool {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	out := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		out[name] = true
	}
	return out
}

// errNoChange makes update skip the write.
var errNoChange = errors.New("no change")

// update is the only write path. statefile.Update re-runs mutate on the newer
// document when the ConfigMap write conflicts (two platform-api pods during a
// rollout), so a counter bump or a status transition decided in mutate holds.
func (s *store) update(mutate func(doc *file) error) error {
	if s.path == "" {
		var doc file
		err := mutate(&doc)
		if errors.Is(err, errNoChange) {
			return nil
		}
		return err
	}
	err := statefile.Update(s.path, func(old []byte) ([]byte, error) {
		doc, err := parse(old)
		if err != nil {
			return nil, err
		}
		if err := mutate(&doc); err != nil {
			return nil, err
		}
		doc.prune()
		return doc.marshal()
	})
	if errors.Is(err, errNoChange) {
		return nil
	}
	return err
}

func (doc *file) marshal() ([]byte, error) {
	if doc.Approvals == nil {
		doc.Approvals = []Approval{}
	}
	for {
		raw, err := doc.marshalOnce()
		if err != nil {
			return nil, err
		}
		if len(raw) <= maxDocBytes || !doc.dropOldestClosed() {
			return raw, nil
		}
	}
}

// marshalOnce writes known fields and copies unknown ones back as raw JSON,
// so a value this binary does not understand is byte-for-value the same.
func (doc *file) marshalOnce() ([]byte, error) {
	rows := make([]json.RawMessage, len(doc.Approvals))
	for i := range doc.Approvals {
		raw, err := marshalApproval(doc.Approvals[i])
		if err != nil {
			return nil, err
		}
		rows[i] = raw
	}
	body := map[string]json.RawMessage{}
	for k, v := range doc.extra {
		body[k] = append(json.RawMessage(nil), v...)
	}
	delete(body, "last_number")
	delete(body, "approvals")
	if doc.LastNumber != 0 {
		raw, err := json.Marshal(doc.LastNumber)
		if err != nil {
			return nil, err
		}
		body["last_number"] = raw
	}
	body["approvals"] = writeRawArray(rows)
	return writeRawObject(body)
}

func marshalApproval(a Approval) (json.RawMessage, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	if len(a.extra) == 0 && !hasNestedExtra(a) {
		return raw, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if a.Execution != nil && len(a.Execution.extra) > 0 {
		merged, err := mergeUnknown(obj["execution"], a.Execution.extra)
		if err != nil {
			return nil, err
		}
		obj["execution"] = merged
	}
	if deliveryExtras(a.Deliveries) {
		rawDels, ok := obj["deliveries"]
		if !ok {
			return nil, errors.New("approvals: deliveries missing while preserving unknown fields")
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(rawDels, &rows); err != nil {
			return nil, err
		}
		if len(rows) != len(a.Deliveries) {
			return nil, errors.New("approvals: deliveries length changed while preserving unknown fields")
		}
		for i := range a.Deliveries {
			merged, err := mergeUnknown(rows[i], a.Deliveries[i].extra)
			if err != nil {
				return nil, err
			}
			rows[i] = merged
		}
		obj["deliveries"] = writeRawArray(rows)
	}
	for k, v := range a.extra {
		if _, known := obj[k]; known {
			continue
		}
		obj[k] = append(json.RawMessage(nil), v...)
	}
	return writeRawObject(obj)
}

func hasNestedExtra(a Approval) bool {
	if a.Execution != nil && len(a.Execution.extra) > 0 {
		return true
	}
	return deliveryExtras(a.Deliveries)
}

func deliveryExtras(ds []Delivery) bool {
	for i := range ds {
		if len(ds[i].extra) > 0 {
			return true
		}
	}
	return false
}

// mergeUnknown splices unknown keys back onto a marshaled object. A known
// key already in raw wins; an unknown value is copied as it was read.
func mergeUnknown(raw json.RawMessage, extra map[string]json.RawMessage) (json.RawMessage, error) {
	if len(extra) == 0 {
		return raw, nil
	}
	obj := map[string]json.RawMessage{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, err
		}
	}
	for k, v := range extra {
		if _, exists := obj[k]; exists {
			continue
		}
		obj[k] = append(json.RawMessage(nil), v...)
	}
	return writeRawObject(obj)
}

// writeRawObject splices values as they were read. encoding/json compacts a
// RawMessage, which would change insignificant space inside a field this
// binary does not understand.
func writeRawObject(fields map[string]json.RawMessage) ([]byte, error) {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		v := fields[k]
		if len(v) == 0 {
			buf.WriteString("null")
			continue
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeRawArray(rows []json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, row := range rows {
		if i > 0 {
			buf.WriteByte(',')
		}
		if len(row) == 0 {
			buf.WriteString("null")
			continue
		}
		buf.Write(row)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// prune keeps every open approval and the most recent keepClosed terminal
// ones; only the newest keepTails terminal ones keep their output tail.
func (doc *file) prune() {
	open := make([]Approval, 0)
	closed := make([]Approval, 0)
	for _, a := range doc.Approvals {
		if a.open() {
			open = append(open, a)
			continue
		}
		closed = append(closed, a)
	}
	sort.SliceStable(closed, func(i, j int) bool {
		return closed[i].closedAt().After(closed[j].closedAt())
	})
	if len(closed) > keepClosed {
		closed = closed[:keepClosed]
	}
	for i := keepTails; i < len(closed); i++ {
		if e := closed[i].Execution; e != nil && e.OutputTail != "" {
			c := *e
			c.OutputTail = ""
			closed[i].Execution = &c
		}
	}
	doc.Approvals = append(open, closed...)
}

// dropOldestClosed removes the last terminal record (prune sorted them newest
// first, after the open ones). false when none is left.
func (doc *file) dropOldestClosed() bool {
	for i := len(doc.Approvals) - 1; i >= 0; i-- {
		if !doc.Approvals[i].open() {
			doc.Approvals = append(doc.Approvals[:i], doc.Approvals[i+1:]...)
			return true
		}
	}
	return false
}

func (doc *file) put(a Approval) {
	for i := range doc.Approvals {
		if doc.Approvals[i].ID == a.ID {
			doc.Approvals[i] = a
			return
		}
	}
	doc.Approvals = append(doc.Approvals, a)
}

// find resolves an approval id or a number ("57" or "#57").
func (doc *file) find(ref string) (Approval, bool) {
	if n, ok := parseNumber(ref); ok {
		for _, a := range doc.Approvals {
			if a.Number == n {
				return a, true
			}
		}
		return Approval{}, false
	}
	for _, a := range doc.Approvals {
		if a.ID == ref {
			return a, true
		}
	}
	return Approval{}, false
}

func (doc *file) nextNumber() int {
	n := doc.LastNumber
	for _, a := range doc.Approvals {
		if a.Number > n {
			n = a.Number
		}
	}
	doc.LastNumber = n + 1
	return doc.LastNumber
}
