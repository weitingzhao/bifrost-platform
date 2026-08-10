package cluster

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

const (
	minioDeployName    = "minio"
	minioContainerName = "minio"
	minioSecretName    = "minio-backup"
	minioBucketName    = "bifrost-postgres-backup"
	minioWalsRelDir    = "bifrost-postgres/wals"
)

var (
	uncompressedHistoryName = regexp.MustCompile(`(?i)^[0-9a-f]{8}\.history$`)
	historyGZName           = regexp.MustCompile(`(?i)^[0-9a-f]{8}\.history\.gz$`)
	safeBackupCRName        = regexp.MustCompile(`^bifrost-postgres-[a-z0-9][-a-z0-9]*$`)
)

// PostgresWalRepairResponse is the Autopilot / Agent actuation for MinIO WAL
// object-store repair + stuck Backup CR cleanup + on-demand Backup.
type PostgresWalRepairResponse struct {
	ActuationResponse
	MinIOReady      bool     `json:"minio_ready"`
	BucketOK        bool     `json:"bucket_ok"`
	ProbeOK         bool     `json:"probe_ok"`
	ClearedObjects  []string `json:"cleared_objects,omitempty"`
	DeletedBackups  []string `json:"deleted_backups,omitempty"`
	TriggeredBackup string   `json:"triggered_backup,omitempty"`
	WalArchiving    string   `json:"wal_archiving,omitempty"`
}

func isStuckBackupPhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "walarchivingfailing", "failed":
		return true
	default:
		return false
	}
}

func isInProgressBackupPhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "started", "running", "waitingforwal", "finalizing":
		return true
	default:
		return false
	}
}

func pickInProgressBackupName(items []unstructured.Unstructured) string {
	for i := range items {
		name := strings.TrimSpace(items[i].GetName())
		if !isSafeBackupCRName(name) {
			continue
		}
		phase := strings.ToLower(strings.TrimSpace(stringFromUnstructured(&items[i], "status", "phase")))
		if isInProgressBackupPhase(phase) {
			return name
		}
	}
	return ""
}

func isUncompressedHistoryObject(name string) bool {
	return uncompressedHistoryName.MatchString(strings.TrimSpace(name))
}

func isHistoryGZObject(name string) bool {
	return historyGZName.MatchString(strings.TrimSpace(name))
}

func isSafeBackupCRName(name string) bool {
	return safeBackupCRName.MatchString(strings.TrimSpace(name))
}

func isSafeMinioRelPath(rel string) bool {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return false
	}
	return true
}

func pickStuckBackupNames(items []unstructured.Unstructured) []string {
	var out []string
	for i := range items {
		name := strings.TrimSpace(items[i].GetName())
		if !isSafeBackupCRName(name) {
			continue
		}
		phase := strings.ToLower(strings.TrimSpace(stringFromUnstructured(&items[i], "status", "phase")))
		if isStuckBackupPhase(phase) {
			out = append(out, name)
		}
	}
	return out
}

func parseWalArchivingCondition(obj *unstructured.Unstructured) (ok bool, detail string) {
	if obj == nil {
		return false, "cluster CR missing"
	}
	raw, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false, "no status.conditions"
	}
	for _, item := range raw {
		m, okm := item.(map[string]any)
		if !okm {
			continue
		}
		typ, _ := m["type"].(string)
		if !strings.EqualFold(typ, "ContinuousArchiving") {
			continue
		}
		status, _ := m["status"].(string)
		reason, _ := m["reason"].(string)
		msg, _ := m["message"].(string)
		detail = strings.TrimSpace(strings.Join([]string{reason, msg}, " · "))
		return strings.EqualFold(status, "True"), detail
	}
	return false, "ContinuousArchiving condition missing"
}

func (s *Service) RepairPostgresWalStore(ctx context.Context) (PostgresWalRepairResponse, error) {
	now := time.Now().UTC()
	resp := PostgresWalRepairResponse{
		ActuationResponse: ActuationResponse{
			Action:      "repair_cnpg_wal_store",
			Target:      cnpgNamespace + "/minio+" + cnpgClusterName,
			GeneratedAt: now,
		},
	}

	if err := s.ensureMinioReady(ctx, &resp); err != nil {
		resp.Message = err.Error()
		return resp, err
	}
	resp.MinIOReady = true

	if err := s.repairMinioWALObjects(ctx, &resp); err != nil {
		resp.Message = err.Error()
		return resp, err
	}

	deleted, err := s.deleteStuckBackupCRs(ctx)
	if err != nil {
		resp.Message = err.Error()
		return resp, err
	}
	resp.DeletedBackups = deleted
	if len(deleted) > 0 {
		resp.Changed = true
	}

	trig, trigErr := s.TriggerPostgresBackup(ctx)
	if trigErr != nil {
		resp.Message = "wal store repaired but trigger backup failed: " + trigErr.Error()
		return resp, trigErr
	}
	resp.TriggeredBackup = strings.TrimPrefix(trig.Target, cnpgNamespace+"/")
	if trig.Changed {
		resp.Changed = true
	}

	resp.WalArchiving = s.readWalArchivingDetail(ctx)
	resp.OK = true
	parts := []string{"minio probe ok"}
	if len(resp.ClearedObjects) > 0 {
		parts = append(parts, fmt.Sprintf("cleared %d object(s)", len(resp.ClearedObjects)))
	}
	if len(resp.DeletedBackups) > 0 {
		parts = append(parts, fmt.Sprintf("deleted stuck Backup %s", strings.Join(resp.DeletedBackups, ",")))
	}
	if resp.TriggeredBackup != "" {
		parts = append(parts, "created "+resp.TriggeredBackup)
	}
	resp.Message = strings.Join(parts, " · ")
	return resp, nil
}

func (s *Service) ensureMinioReady(ctx context.Context, resp *PostgresWalRepairResponse) error {
	clientset, _, err := s.buildClient()
	if err != nil {
		return err
	}
	pod, err := s.minioPodName(ctx, clientset)
	if err != nil {
		// Best-effort restart then retry once.
		_, _ = s.RolloutRestart(ctx, RolloutRestartRequest{
			Namespace: cnpgNamespace, Kind: "Deployment", Name: minioDeployName,
		})
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			pod, err = s.minioPodName(ctx, clientset)
			if err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("minio not ready: %w", err)
		}
		resp.Changed = true
	}
	_ = pod
	return nil
}

func (s *Service) minioPodName(ctx context.Context, clientset kubernetes.Interface) (string, error) {
	list, err := clientset.CoreV1().Pods(cnpgNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=minio",
	})
	if err != nil {
		return "", err
	}
	for i := range list.Items {
		p := &list.Items[i]
		if p.DeletionTimestamp != nil {
			continue
		}
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		ready := false
		for _, c := range p.Status.ContainerStatuses {
			if c.Name == minioContainerName && c.Ready {
				ready = true
				break
			}
		}
		if ready {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no Ready minio pod in %s", cnpgNamespace)
}

func (s *Service) minioCredentials(ctx context.Context) (accessKey, secretKey string, err error) {
	clientset, _, err := s.buildClient()
	if err != nil {
		return "", "", err
	}
	sec, err := clientset.CoreV1().Secrets(cnpgNamespace).Get(ctx, minioSecretName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("secret %s/%s: %w", cnpgNamespace, minioSecretName, err)
	}
	ak := strings.TrimSpace(string(sec.Data["ACCESS_KEY_ID"]))
	sk := strings.TrimSpace(string(sec.Data["SECRET_ACCESS_KEY"]))
	if ak == "" || sk == "" {
		return "", "", fmt.Errorf("secret %s missing ACCESS_KEY_ID/SECRET_ACCESS_KEY", minioSecretName)
	}
	return ak, sk, nil
}

func (s *Service) execOnMinio(ctx context.Context, command ...string) (string, error) {
	if fn := s.podExec(); fn != nil {
		kubeconfig := s.kubeconfigPath()
		return fn(ctx, kubeconfig, cnpgNamespace, minioDeployName, minioContainerName, command...)
	}
	clientset, _, err := s.buildClient()
	if err != nil {
		return "", err
	}
	pod, err := s.minioPodName(ctx, clientset)
	if err != nil {
		return "", err
	}
	return s.execViaAPI(ctx, cnpgNamespace, pod, minioContainerName, command...)
}

func (s *Service) repairMinioWALObjects(ctx context.Context, resp *PostgresWalRepairResponse) error {
	ak, sk, err := s.minioCredentials(ctx)
	if err != nil {
		return err
	}
	if _, err := s.execOnMinio(ctx, "mkdir", "-p", "/data/"+minioBucketName); err != nil {
		return fmt.Errorf("ensure bucket dir: %w", err)
	}
	if _, err := s.execOnMinio(ctx, "mc", "alias", "set", "local", "http://127.0.0.1:9000", ak, sk, "--api", "S3v4"); err != nil {
		return fmt.Errorf("mc alias: %w", err)
	}
	if _, err := s.execOnMinio(ctx, "mc", "mb", "--ignore-existing", "local/"+minioBucketName); err != nil {
		// Older mc uses -p; ignore if bucket already exists.
		if !strings.Contains(strings.ToLower(err.Error()), "already exist") {
			_, _ = s.execOnMinio(ctx, "mc", "mb", "-p", "local/"+minioBucketName)
		}
	}
	resp.BucketOK = true

	probeKey := fmt.Sprintf("local/%s/.bifrost-wal-probe-%d", minioBucketName, time.Now().UTC().Unix())
	if _, err := s.execOnMinio(ctx, "sh", "-c", fmt.Sprintf("printf probe | mc pipe %s", probeKey)); err != nil {
		return fmt.Errorf("minio put probe: %w", err)
	}
	_, _ = s.execOnMinio(ctx, "mc", "rm", "--force", probeKey)
	resp.ProbeOK = true

	walsDir := "/data/" + minioBucketName + "/" + minioWalsRelDir
	listing, lsErr := s.execOnMinio(ctx, "ls", "-1", walsDir)
	if lsErr != nil {
		// Empty / missing wals prefix is fine — first backup will create it.
		return nil
	}
	for _, name := range strings.Split(listing, "\n") {
		name = strings.TrimSpace(name)
		rel := minioWalsRelDir + "/" + name
		if !isSafeMinioRelPath(rel) {
			continue
		}
		fsPath := "/data/" + minioBucketName + "/" + rel
		s3Path := "local/" + minioBucketName + "/" + rel

		// Uncompressed *.history collides with barman gzip key *.history.gz on MinIO FS.
		if isUncompressedHistoryObject(name) {
			_, _ = s.execOnMinio(ctx, "mc", "rm", "--force", "--recursive", s3Path)
			if _, rmErr := s.execOnMinio(ctx, "rm", "-rf", fsPath); rmErr != nil {
				return fmt.Errorf("clear uncompressed history %s: %w", name, rmErr)
			}
			resp.ClearedObjects = append(resp.ClearedObjects, rel)
			resp.Changed = true
			continue
		}

		// Orphan xl.meta: object unlistable via S3 but directory remains → AccessDenied on PutObject.
		if isHistoryGZObject(name) {
			if _, statErr := s.execOnMinio(ctx, "mc", "stat", s3Path); statErr == nil {
				continue
			}
			_, _ = s.execOnMinio(ctx, "mc", "rm", "--force", "--recursive", s3Path)
			if _, rmErr := s.execOnMinio(ctx, "rm", "-rf", fsPath); rmErr != nil {
				return fmt.Errorf("clear orphan history.gz %s: %w", name, rmErr)
			}
			resp.ClearedObjects = append(resp.ClearedObjects, rel+" (orphan)")
			resp.Changed = true
		}
	}
	return nil
}

func (s *Service) deleteStuckBackupCRs(ctx context.Context) ([]string, error) {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	list, err := dyn.Resource(cnpgBackupGVR).Namespace(cnpgNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	names := pickStuckBackupNames(list.Items)
	var deleted []string
	for _, name := range names {
		if err := dyn.Resource(cnpgBackupGVR).Namespace(cnpgNamespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			return deleted, fmt.Errorf("delete Backup %s: %w", name, err)
		}
		deleted = append(deleted, name)
	}
	return deleted, nil
}

func (s *Service) readWalArchivingDetail(ctx context.Context) string {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return err.Error()
	}
	obj, err := dyn.Resource(cnpgClusterGVR).Namespace(cnpgNamespace).Get(ctx, cnpgClusterName, metav1.GetOptions{})
	if err != nil {
		return err.Error()
	}
	ok, detail := parseWalArchivingCondition(obj)
	if ok {
		if detail == "" {
			return "ok"
		}
		return "ok · " + detail
	}
	if detail == "" {
		return "fail"
	}
	return "fail · " + detail
}
