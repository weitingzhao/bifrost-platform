package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	backupFreshMaxAge = 48 * time.Hour
	backupFreshHours  = 48
)

var cnpgBackupGVR = schema.GroupVersionResource{
	Group: "postgresql.cnpg.io", Version: "v1", Resource: "backups",
}

// PostgresBackupStatusResponse is the freshness probe for CNPG Backup CRs.
type PostgresBackupStatusResponse struct {
	Fresh              bool      `json:"fresh"`
	Signal             string    `json:"signal"`
	Detail             string    `json:"detail"`
	LastCompletedAt    string    `json:"last_completed_at,omitempty"`
	LastBackupName     string    `json:"last_backup_name,omitempty"`
	LastBackupPhase    string    `json:"last_backup_phase,omitempty"`
	MaxAgeHours        int       `json:"max_age_hours"`
	AgeHours           *float64  `json:"age_hours,omitempty"`
	BackupCount        int       `json:"backup_count"`
	StuckBackups       []string  `json:"stuck_backups,omitempty"`
	WalArchivingOK     *bool     `json:"wal_archiving_ok,omitempty"`
	WalArchivingDetail string    `json:"wal_archiving_detail,omitempty"`
	GeneratedAt        time.Time `json:"generated_at"`
}

type completedBackup struct {
	Name      string
	Phase     string
	StoppedAt time.Time
}

func (s *Service) PostgresBackupStatus(ctx context.Context) PostgresBackupStatusResponse {
	now := time.Now().UTC()
	resp := PostgresBackupStatusResponse{
		MaxAgeHours: backupFreshHours,
		GeneratedAt: now,
	}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		resp.Signal = "fail"
		resp.Detail = "kube client unavailable: " + err.Error()
		return resp
	}
	list, err := dyn.Resource(cnpgBackupGVR).Namespace(cnpgNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		resp.Signal = "fail"
		resp.Detail = "list CNPG backups: " + err.Error()
		return resp
	}
	resp.BackupCount = len(list.Items)
	latest := pickLatestCompletedBackup(list.Items)
	if latest != nil {
		resp.LastBackupName = latest.Name
		resp.LastBackupPhase = latest.Phase
		resp.LastCompletedAt = latest.StoppedAt.UTC().Format(time.RFC3339)
		age := now.Sub(latest.StoppedAt)
		hours := age.Hours()
		resp.AgeHours = &hours
	}
	var stopped *time.Time
	if latest != nil {
		t := latest.StoppedAt
		stopped = &t
	}
	resp.Fresh, resp.Signal, resp.Detail = classifyBackupFreshness(stopped, now, backupFreshMaxAge)
	if latest != nil && resp.LastBackupName != "" && !strings.Contains(resp.Detail, resp.LastBackupName) {
		resp.Detail = fmt.Sprintf("%s · %s", resp.LastBackupName, resp.Detail)
	}
	resp.StuckBackups = pickStuckBackupNames(list.Items)
	if len(resp.StuckBackups) > 0 {
		resp.Detail = fmt.Sprintf("%s · stuck Backup %s", resp.Detail, strings.Join(resp.StuckBackups, ","))
		if resp.Signal == "ok" {
			resp.Signal = "degraded"
			resp.Fresh = false
		}
	}
	if clusterObj, cErr := dyn.Resource(cnpgClusterGVR).Namespace(cnpgNamespace).Get(ctx, cnpgClusterName, metav1.GetOptions{}); cErr == nil {
		ok, detail := parseWalArchivingCondition(clusterObj)
		resp.WalArchivingOK = &ok
		resp.WalArchivingDetail = detail
		if !ok {
			if resp.Signal == "ok" {
				resp.Signal = "degraded"
				resp.Fresh = false
			}
			if detail != "" && !strings.Contains(resp.Detail, detail) {
				resp.Detail = fmt.Sprintf("%s · WAL archive: %s", resp.Detail, detail)
			}
		}
	}
	return resp
}

func (s *Service) TriggerPostgresBackup(ctx context.Context) (ActuationResponse, error) {
	now := time.Now().UTC()
	name := fmt.Sprintf("bifrost-postgres-ondemand-%s", now.Format("20060102-150405"))
	resp := ActuationResponse{
		Action:      "trigger_cnpg_backup",
		Target:      cnpgNamespace + "/" + name,
		GeneratedAt: now,
	}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		resp.Message = err.Error()
		return resp, err
	}
	if list, lerr := dyn.Resource(cnpgBackupGVR).Namespace(cnpgNamespace).List(ctx, metav1.ListOptions{}); lerr == nil {
		if running := pickInProgressBackupName(list.Items); running != "" {
			resp.OK = true
			resp.Target = cnpgNamespace + "/" + running
			resp.Message = "backup already in progress: " + running
			return resp, nil
		}
	}
	obj := newOnDemandBackupCR(name, now)
	created, err := dyn.Resource(cnpgBackupGVR).Namespace(cnpgNamespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			resp.OK = true
			resp.Message = "backup CR already exists"
			return resp, nil
		}
		resp.Message = err.Error()
		return resp, err
	}
	resp.OK = true
	resp.Changed = true
	resp.Message = fmt.Sprintf("created Backup %s", created.GetName())
	return resp, nil
}

func classifyBackupFreshness(lastCompleted *time.Time, now time.Time, maxAge time.Duration) (fresh bool, signal, detail string) {
	if lastCompleted == nil || lastCompleted.IsZero() {
		return false, "fail", "no completed CNPG Backup"
	}
	age := now.Sub(*lastCompleted)
	if age < 0 {
		age = 0
	}
	if age <= maxAge {
		return true, "ok", fmt.Sprintf("last completed %s ago", formatBackupAge(age))
	}
	return false, "fail", fmt.Sprintf("last completed %s ago (limit %s)", formatBackupAge(age), formatBackupAge(maxAge))
}

func pickLatestCompletedBackup(items []unstructured.Unstructured) *completedBackup {
	var best *completedBackup
	for i := range items {
		parsed := parseCompletedBackup(&items[i])
		if parsed == nil {
			continue
		}
		if best == nil || parsed.StoppedAt.After(best.StoppedAt) {
			cp := *parsed
			best = &cp
		}
	}
	return best
}

func parseCompletedBackup(obj *unstructured.Unstructured) *completedBackup {
	phase := strings.ToLower(strings.TrimSpace(stringFromUnstructured(obj, "status", "phase")))
	if phase != "completed" {
		return nil
	}
	raw := stringFromUnstructured(obj, "status", "stoppedAt")
	if raw == "" {
		raw = stringFromUnstructured(obj, "status", "startedAt")
	}
	stopped, err := parseBackupTimestamp(raw)
	if err != nil {
		return nil
	}
	return &completedBackup{
		Name:      obj.GetName(),
		Phase:     phase,
		StoppedAt: stopped,
	}
}

func parseBackupTimestamp(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("parse timestamp %q", raw)
}

func formatBackupAge(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Minute).String()
}

func newOnDemandBackupCR(name string, now time.Time) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Backup",
			"metadata": map[string]any{
				"name":      name,
				"namespace": cnpgNamespace,
				"labels": map[string]any{
					"app.kubernetes.io/name":    "bifrost-postgres",
					"app.kubernetes.io/part-of": "bifrost",
					"bifrost.dev/triggered-by":  "ops-autopilot",
					"bifrost.dev/triggered-at":  now.UTC().Format("20060102-150405"),
				},
			},
			"spec": map[string]any{
				"method": "barmanObjectStore",
				"cluster": map[string]any{
					"name": cnpgClusterName,
				},
			},
		},
	}
}
