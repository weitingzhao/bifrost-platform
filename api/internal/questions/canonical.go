package questions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Canonical is the one normalized form of a create request.
// JSON is sorted, compact, UTF-8, and not HTML-escaped. The idempotency key
// is not part of JSON. RequestFP hashes JSON. QuestionFP hashes the
// "what is being asked" subset. Recommended in JSON is the 1-based index.
type Canonical struct {
	JSON        string
	RequestFP   string
	QuestionFP  string
	Key         string
	Title       string
	Body        string
	Options     []Option
	Recommended string
	AllowText   bool
	Urgency     string
	Meanwhile   string
	WorkID      string
	OwnerDuty   string
	TaskID      string
	Thread      string
	Link        string
	LinkText    string
	TTLHours    int
}

var (
	// A business issue's number (BI-<n>) names the cards the agent runtime
	// asks the Owner on it (W-31 BI-1 slice 2, issuecard.go); create takes
	// it from the runtime's own session only.
	workIDPattern = regexp.MustCompile(`^(W-[0-9]+|TD-[0-9]+|LANE-[A-Za-z0-9-]+|BI-[1-9][0-9]*)$`)
	// taskIDPattern is an agent runtime task id: t_ and a 128-bit ULID.
	taskIDPattern  = regexp.MustCompile(`^t_[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	keyPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
	sessionPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

type fieldError struct {
	Field string
	Msg   string
}

func (e *fieldError) Error() string { return e.Msg }

func fieldErr(field, msg string) error {
	return &fieldError{Field: field, Msg: msg}
}

type createJSON struct {
	IdempotencyKey  json.RawMessage `json:"idempotency_key"`
	Title           json.RawMessage `json:"title"`
	Body            json.RawMessage `json:"body"`
	Options         json.RawMessage `json:"options"`
	Recommended     json.RawMessage `json:"recommended"`
	AllowText       json.RawMessage `json:"allow_text"`
	Urgency         json.RawMessage `json:"urgency"`
	Meanwhile       json.RawMessage `json:"meanwhile"`
	WorkID          json.RawMessage `json:"work_id"`
	OwnerDuty       json.RawMessage `json:"owner_duty"`
	TaskID          json.RawMessage `json:"task_id"`
	RequesterThread json.RawMessage `json:"requester_thread"`
	Link            json.RawMessage `json:"link"`
	LinkText        json.RawMessage `json:"link_text"`
	TTLHours        json.RawMessage `json:"ttl_hours"`
}

type optionJSON struct {
	Label  json.RawMessage `json:"label"`
	Detail json.RawMessage `json:"detail"`
}

// CanonicalizeRequestJSON validates a create body and returns its canonical form.
// The key is required, not trimmed, and absent from the canonical JSON.
func CanonicalizeRequestJSON(raw []byte) (Canonical, error) {
	var in createJSON
	if err := decodeStrict(raw, &in); err != nil {
		return Canonical{}, err
	}
	return buildCanonical(in, true)
}

// CanonicalizeObjectJSON is CanonicalizeRequestJSON without requiring a key.
// The shared test vectors call it.
func CanonicalizeObjectJSON(raw []byte) (Canonical, error) {
	var in createJSON
	if err := decodeStrict(raw, &in); err != nil {
		return Canonical{}, err
	}
	return buildCanonical(in, false)
}

// decodeStrict accepts one JSON value and trailing whitespace only.
// Unknown fields are refused. A second value or any other trailer is an error.
func decodeStrict(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return decodeErr(err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fieldErr("body", "invalid json body")
	}
	return nil
}

func buildCanonical(in createJSON, requireKey bool) (Canonical, error) {
	var c Canonical
	if requireKey || len(in.IdempotencyKey) > 0 {
		key, ok, err := rawString(in.IdempotencyKey)
		if err != nil {
			return Canonical{}, fieldErr("idempotency_key", "idempotency_key must be a string")
		}
		if requireKey && !ok {
			return Canonical{}, fieldErr("idempotency_key", "idempotency_key is required")
		}
		if ok && !keyPattern.MatchString(key) {
			return Canonical{}, fieldErr("idempotency_key", "idempotency_key is invalid")
		}
		c.Key = key
	}
	title, ok, err := cleanedString(in.Title, false)
	if err != nil {
		return Canonical{}, fieldErr("title", "title must be a string")
	}
	if !ok {
		return Canonical{}, fieldErr("title", "title is required")
	}
	if err := limit(title, "title", titleLimit); err != nil {
		return Canonical{}, err
	}
	c.Title = title

	body, _, err := cleanedString(in.Body, true)
	if err != nil {
		return Canonical{}, fieldErr("body", "body must be a string")
	}
	if err := limit(body, "body", bodyLimit); err != nil {
		return Canonical{}, err
	}
	c.Body = body

	opts, err := buildOptions(in.Options)
	if err != nil {
		return Canonical{}, err
	}
	c.Options = opts

	rec, hasRec, err := decodeInt(in.Recommended, "recommended")
	if err != nil {
		return Canonical{}, err
	}
	if hasRec && rec != 0 && (rec < 1 || rec > len(opts)) {
		return Canonical{}, fieldErr("recommended", "recommended is out of range")
	}
	if rec < 0 {
		return Canonical{}, fieldErr("recommended", "recommended is out of range")
	}
	if rec > 0 {
		c.Recommended = string(rune('a' + rec - 1))
	}

	allow, hasAllow, err := decodeBool(in.AllowText, "allow_text")
	if err != nil {
		return Canonical{}, err
	}
	if len(opts) == 0 {
		if hasAllow && !allow {
			return Canonical{}, fieldErr("allow_text", "allow_text must be true when there are no options")
		}
		c.AllowText = true
	} else if hasAllow {
		c.AllowText = allow
	}

	urgency, ok, err := cleanedString(in.Urgency, false)
	if err != nil {
		return Canonical{}, fieldErr("urgency", "urgency must be a string")
	}
	if !ok {
		return Canonical{}, fieldErr("urgency", "urgency is required")
	}
	if err := limit(urgency, "urgency", lineLimit); err != nil {
		return Canonical{}, err
	}
	c.Urgency = urgency

	meanwhile, ok, err := cleanedString(in.Meanwhile, false)
	if err != nil {
		return Canonical{}, fieldErr("meanwhile", "meanwhile must be a string")
	}
	if !ok {
		return Canonical{}, fieldErr("meanwhile", "meanwhile is required")
	}
	if err := limit(meanwhile, "meanwhile", lineLimit); err != nil {
		return Canonical{}, err
	}
	c.Meanwhile = meanwhile

	work, _, err := cleanedString(in.WorkID, false)
	if err != nil {
		return Canonical{}, fieldErr("work_id", "work_id must be a string")
	}
	if work != "" && (encodedBytes(work) > workLimit || !workIDPattern.MatchString(work)) {
		return Canonical{}, fieldErr("work_id", "work_id is invalid")
	}
	c.WorkID = work

	// A decision card (design v5 §7 item 3): which of the Owner's three
	// duties the question is (set a goal, set a rule, something
	// irreversible) and the agent runtime task it is about.
	duty, _, err := cleanedString(in.OwnerDuty, false)
	if err != nil || (duty != "" && !OwnerDuties[duty]) {
		return Canonical{}, fieldErr("owner_duty", "owner_duty is goal, rule or irreversible")
	}
	c.OwnerDuty = duty
	task, _, err := cleanedString(in.TaskID, false)
	if err != nil || (task != "" && !taskIDPattern.MatchString(task)) {
		return Canonical{}, fieldErr("task_id", "task_id is not an agent runtime task id")
	}
	c.TaskID = task

	thread, _, err := cleanedString(in.RequesterThread, false)
	if err != nil {
		return Canonical{}, fieldErr("requester_thread", "requester_thread must be a string")
	}
	if err := limit(thread, "requester_thread", threadLimit); err != nil {
		return Canonical{}, err
	}
	c.Thread = thread

	link, _, err := cleanedString(in.Link, false)
	if err != nil {
		return Canonical{}, fieldErr("link", "link must be a string")
	}
	if err := limit(link, "link", linkLimit); err != nil {
		return Canonical{}, err
	}
	if linkForbidden(link) {
		return Canonical{}, fieldErr("link", "link is not allowed")
	}
	c.Link = link

	linkText, _, err := cleanedString(in.LinkText, false)
	if err != nil {
		return Canonical{}, fieldErr("link_text", "link_text must be a string")
	}
	if err := limit(linkText, "link_text", linkTextLimit); err != nil {
		return Canonical{}, err
	}
	if c.Link == "" {
		linkText = ""
	}
	c.LinkText = linkText

	ttl, hasTTL, err := decodeInt(in.TTLHours, "ttl_hours")
	if err != nil {
		return Canonical{}, err
	}
	if !hasTTL {
		ttl = defaultTTLHours
	} else if ttl < 1 || ttl > maxTTLHours {
		return Canonical{}, fieldErr("ttl_hours", "ttl_hours must be from 1 to 168")
	}
	c.TTLHours = ttl

	optJSON := optionsCanonical(opts)
	m := map[string]any{
		"allow_text": c.AllowText,
		"meanwhile":  c.Meanwhile,
		"title":      c.Title,
		"ttl_hours":  c.TTLHours,
		"urgency":    c.Urgency,
	}
	if c.Body != "" {
		m["body"] = c.Body
	}
	if optJSON != nil {
		m["options"] = optJSON
	}
	if rec > 0 {
		m["recommended"] = rec
	}
	if c.Thread != "" {
		m["requester_thread"] = c.Thread
	}
	if c.WorkID != "" {
		m["work_id"] = c.WorkID
	}
	if c.OwnerDuty != "" {
		m["owner_duty"] = c.OwnerDuty
	}
	if c.TaskID != "" {
		m["task_id"] = c.TaskID
	}
	if c.Link != "" {
		m["link"] = c.Link
	}
	if c.LinkText != "" {
		m["link_text"] = c.LinkText
	}
	raw, err := marshalJSON(m)
	if err != nil {
		return Canonical{}, err
	}
	c.JSON = string(raw)
	c.RequestFP = sha256hex(raw)

	q := map[string]any{
		"allow_text": c.AllowText,
		"title":      c.Title,
	}
	if c.Body != "" {
		q["body"] = c.Body
	}
	if optJSON != nil {
		q["options"] = optJSON
	}
	if c.OwnerDuty != "" {
		q["owner_duty"] = c.OwnerDuty
	}
	if c.TaskID != "" {
		q["task_id"] = c.TaskID
	}
	qraw, err := marshalJSON(q)
	if err != nil {
		return Canonical{}, err
	}
	c.QuestionFP = sha256hex(qraw)
	return c, nil
}

func optionsCanonical(opts []Option) []any {
	if len(opts) == 0 {
		return nil
	}
	out := make([]any, len(opts))
	for i, o := range opts {
		m := map[string]any{"label": o.Label}
		if o.Detail != "" {
			m["detail"] = o.Detail
		}
		out[i] = m
	}
	return out
}

func buildOptions(raw json.RawMessage) ([]Option, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var rows []optionJSON
	if err := dec.Decode(&rows); err != nil {
		if fe, ok := asUnknown(err); ok {
			return nil, fe
		}
		return nil, fieldErr("options", "options must be a list")
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) < 2 || len(rows) > 4 {
		return nil, fieldErr("options", "options must be none or 2 to 4")
	}
	out := make([]Option, len(rows))
	for i, row := range rows {
		label, ok, err := cleanedString(row.Label, false)
		if err != nil {
			return nil, fieldErr("label", "label must be a string")
		}
		if !ok {
			return nil, fieldErr("label", "label is required")
		}
		if err := limit(label, "label", labelLimit); err != nil {
			return nil, err
		}
		detail, _, err := cleanedString(row.Detail, false)
		if err != nil {
			return nil, fieldErr("detail", "detail must be a string")
		}
		if err := limit(detail, "detail", detailLimit); err != nil {
			return nil, err
		}
		out[i] = Option{Key: string(rune('a' + i)), Label: label, Detail: detail}
	}
	return out, nil
}

// DefaultKey is hex(sha256(canonical({session, request}))). session is not trimmed.
func DefaultKey(session, requestCanonical string) string {
	payload := `{"request":` + requestCanonical + `,"session":` + jsonString(session) + `}`
	return sha256hex([]byte(payload))
}

func jsonString(s string) string {
	raw, err := marshalJSON(s)
	if err != nil {
		return `""`
	}
	return string(raw)
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func requesterHash(s string) string {
	return sha256hex([]byte(s))
}

func cleanedString(raw json.RawMessage, keepNewlines bool) (string, bool, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return "", false, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false, err
	}
	s = clean(s, keepNewlines)
	if s == "" {
		return "", false, nil
	}
	return s, true, nil
}

func rawString(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return "", false, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false, err
	}
	return s, true, nil
}

func decodeInt(raw json.RawMessage, field string) (int, bool, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return 0, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var n int
	if err := dec.Decode(&n); err != nil {
		return 0, false, fieldErr(field, field+" must be an integer")
	}
	return n, true, nil
}

func decodeBool(raw json.RawMessage, field string) (bool, bool, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return false, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var v bool
	if err := dec.Decode(&v); err != nil {
		return false, false, fieldErr(field, field+" must be a boolean")
	}
	return v, true, nil
}

func limit(s, field string, max int) error {
	if s == "" {
		return nil
	}
	if encodedBytes(s) > max {
		return fieldErr(field, fmt.Sprintf("%s exceeds %d bytes", field, max))
	}
	return nil
}

func decodeErr(err error) error {
	if fe, ok := asUnknown(err); ok {
		return fe
	}
	if err == nil {
		return fieldErr("body", "invalid json body")
	}
	return fieldErr("body", "invalid json body")
}

func asUnknown(err error) (*fieldError, bool) {
	if err == nil {
		return nil, false
	}
	msg := err.Error()
	const p = `json: unknown field `
	if i := strings.Index(msg, p); i >= 0 {
		name := strings.Trim(msg[i+len(p):], `"`)
		if sp := strings.IndexAny(name, " \n"); sp >= 0 {
			name = name[:sp]
		}
		return &fieldError{Field: name, Msg: "unknown field " + name}, true
	}
	return nil, false
}

// ValidSession reports whether the X-Bifrost-Session header may be stored.
// The value is not trimmed.
func ValidSession(s string) error {
	if s == "" {
		return fieldErr("X-Bifrost-Session", "X-Bifrost-Session is required")
	}
	if len(s) > sessionLimit || !sessionPattern.MatchString(s) {
		return fieldErr("X-Bifrost-Session", "X-Bifrost-Session is invalid")
	}
	return nil
}
