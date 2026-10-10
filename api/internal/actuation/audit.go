package actuation

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

type AuditRecord struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Role   Role      `json:"role"`
	Action string    `json:"action"`
	Target string    `json:"target"`
	Status string    `json:"status"`
	Detail string    `json:"detail"`
}

type AuditLog struct {
	mu      sync.Mutex
	path    string
	records []AuditRecord
	// also lists the records other processes keep (the workers pod's audit,
	// read by the api pod that serves GET /audit)
	alsoList []string
}

const maxAuditRecords = 500

// AlsoList makes HandleList merge the records stored at these paths (written by
// another process with its own AuditLog) into its answer.
func (l *AuditLog) AlsoList(paths ...string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.alsoList = append(l.alsoList, paths...)
}

// emit writes one log line per record: the stored audit keeps the last
// maxAuditRecords, the log pipeline (Loki) keeps the long tail (TD-196).
func emit(r AuditRecord) {
	slog.Info("audit", "actor", r.Actor, "role", r.Role, "action", r.Action,
		"target", r.Target, "status", r.Status, "detail", r.Detail)
}

func NewAuditLog(path string) *AuditLog {
	if path == "" {
		path = os.Getenv("PLATFORM_AUDIT_LOG")
	}
	log := &AuditLog{path: path}
	_ = log.load()
	return log
}

func (l *AuditLog) Record(r *http.Request, action, target, status, detail string) {
	if l == nil {
		return
	}
	detail = approvalnotify.Redact(detail)
	principal := PrincipalFromContext(r.Context())
	record := AuditRecord{
		ID:     fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
		At:     time.Now().UTC(),
		Actor:  principal.Name,
		Role:   principal.Role,
		Action: action,
		Target: target,
		Status: status,
		Detail: detail,
	}
	emit(record)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append([]AuditRecord{record}, l.records...)
	if len(l.records) > maxAuditRecords {
		l.records = l.records[:maxAuditRecords]
	}
	_ = l.persistLocked()
}

func (l *AuditLog) RecordDirect(actor string, role Role, action, target, status, detail string) {
	if l == nil {
		return
	}
	detail = approvalnotify.Redact(detail)
	record := AuditRecord{
		ID:     fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
		At:     time.Now().UTC(),
		Actor:  actor,
		Role:   role,
		Action: action,
		Target: target,
		Status: status,
		Detail: detail,
	}
	emit(record)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append([]AuditRecord{record}, l.records...)
	if len(l.records) > maxAuditRecords {
		l.records = l.records[:maxAuditRecords]
	}
	_ = l.persistLocked()
}

type AuditAppendRequest struct {
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// HandleAppend ingests satellite actuation records (Trade api-monitor market-ingest).
// Requires operator Bearer token; role comes from the authenticated principal.
func (l *AuditLog) HandleAppend(w http.ResponseWriter, r *http.Request) {
	if l == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audit log not configured"})
		return
	}
	var body AuditAppendRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		if err == io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty body"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(body.Action) == "" || strings.TrimSpace(body.Target) == "" || strings.TrimSpace(body.Status) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action, target, and status are required"})
		return
	}
	principal := PrincipalFromContext(r.Context())
	actor := strings.TrimSpace(body.Actor)
	if actor == "" {
		actor = principal.Name
	}
	record := AuditRecord{
		ID:     fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
		At:     time.Now().UTC(),
		Actor:  actor,
		Role:   principal.Role,
		Action: strings.TrimSpace(body.Action),
		Target: strings.TrimSpace(body.Target),
		Status: strings.TrimSpace(body.Status),
		Detail: body.Detail,
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append([]AuditRecord{record}, l.records...)
	if len(l.records) > 500 {
		l.records = l.records[:500]
	}
	_ = l.persistLocked()
	writeJSON(w, http.StatusOK, map[string]any{"id": record.ID, "at": record.At})
}

func (l *AuditLog) HandleList(w http.ResponseWriter, _ *http.Request) {
	l.mu.Lock()
	records := append([]AuditRecord(nil), l.records...)
	others := append([]string(nil), l.alsoList...)
	l.mu.Unlock()
	if len(others) > 0 {
		seen := make(map[string]bool, len(records))
		for _, r := range records {
			seen[r.ID+"|"+r.Action] = true
		}
		for _, path := range others {
			data, err := statefile.ReadFile(path)
			if err != nil {
				continue
			}
			var more []AuditRecord
			if json.Unmarshal(data, &more) != nil {
				continue
			}
			for _, r := range more {
				if !seen[r.ID+"|"+r.Action] {
					seen[r.ID+"|"+r.Action] = true
					records = append(records, r)
				}
			}
		}
		sort.SliceStable(records, func(i, j int) bool { return records[i].At.After(records[j].At) })
		if len(records) > maxAuditRecords {
			records = records[:maxAuditRecords]
		}
	}
	if records == nil {
		records = []AuditRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (l *AuditLog) load() error {
	if l.path == "" {
		return nil
	}
	data, err := statefile.ReadFile(l.path)
	if err != nil {
		return nil
	}
	var records []AuditRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	l.records = records
	return nil
}

func (l *AuditLog) persistLocked() error {
	if l.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(l.records, "", "  ")
	if err != nil {
		return err
	}
	return statefile.WriteFile(l.path, data, 0o600)
}
