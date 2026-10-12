package releasefreeze

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// displayOnly is the tolerant reading. It never returns clear: a refusal stays a refusal.
func displayOnly(status int, body []byte) Decision {
	if status != 200 {
		return notClear(fmt.Sprintf("HTTP %d", status))
	}
	recs, ok := displayRecords(body)
	if !ok || len(recs) == 0 {
		return notClear("not a member of L")
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	lines := make([]string, 0, len(recs)+1)
	lines = append(lines, fmt.Sprintf(lineFrozen, len(recs)))
	for _, rec := range recs {
		lines = append(lines, fmt.Sprintf("%s  who=%s  at=%s  reason=%s",
			rec.Name, flat(rec.Who), flat(rec.At), flat(rec.Reason)))
	}
	d := Decision{Verdict: VerdictFrozen, Records: recs, Lines: lines}
	if d.Verdict == VerdictClear {
		return notClear("not a member of L")
	}
	return d
}

func flat(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

func displayRecords(body []byte) ([]Record, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil {
		return nil, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, false
	}
	raw, ok := top["items"]
	if !ok {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false
	}
	recs := make([]Record, 0, len(items))
	for _, item := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			return nil, false
		}
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(obj["metadata"], &meta); err != nil {
			return nil, false
		}
		name, ok := jsonString(meta["name"])
		if !ok || name == "" {
			return nil, false
		}
		uid, _ := jsonString(meta["uid"])
		var spec map[string]json.RawMessage
		_ = json.Unmarshal(obj["spec"], &spec)
		via, _ := jsonString(spec["via"])
		recs = append(recs, Record{
			Name:   name,
			UID:    uid,
			Who:    jsonField(spec, "who"),
			Reason: jsonField(spec, "reason"),
			At:     jsonField(spec, "at"),
			Via:    via,
		})
	}
	return recs, true
}

func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func jsonField(spec map[string]json.RawMessage, key string) string {
	s, ok := jsonString(spec[key])
	if !ok {
		return "?"
	}
	return s
}
