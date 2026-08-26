package research

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CronJobTrigger catalog — whitelist only (Wave R8 L1).
var cronJobTriggers = map[string]string{
	"dbt-sepa":           "bifrost-analytics-daily",
	"momentum":           "research-engines-momentum",
	"iv-percentile":      "research-iv-percentile",
	"terrain-forecast":   "research-engines-forecast",
	"terrain-intraday":   "research-terrain-intraday",
	"gex-intraday":       "research-gex-intraday",
	"event-radar":        "research-engines-event-radar",
}

type CronJobTriggerResponse struct {
	OK         bool      `json:"ok"`
	JobName    string    `json:"job_name"`
	CronJob    string    `json:"cronjob"`
	Namespace  string    `json:"namespace"`
	StartedAt  time.Time `json:"started_at"`
	Message    string    `json:"message,omitempty"`
	TriggerID  string    `json:"trigger_id"`
	GeneratedAt time.Time `json:"generated_at"`
}

func (s *Service) TriggerCronJob(ctx context.Context, triggerID string) (CronJobTriggerResponse, error) {
	now := time.Now().UTC()
	id := strings.TrimSpace(triggerID)
	cronName, ok := cronJobTriggers[id]
	if !ok {
		return CronJobTriggerResponse{
			OK:          false,
			TriggerID:   id,
			Message:     "unknown trigger id (not in whitelist)",
			GeneratedAt: now,
		}, fmt.Errorf("unknown research cronjob trigger: %s", id)
	}

	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return CronJobTriggerResponse{
			OK:          false,
			TriggerID:   id,
			CronJob:     cronName,
			Namespace:   pluginNamespace,
			Message:     err.Error(),
			GeneratedAt: now,
		}, err
	}

	cj, err := clientset.BatchV1().CronJobs(pluginNamespace).Get(ctx, cronName, metav1.GetOptions{})
	if err != nil {
		return CronJobTriggerResponse{
			OK:          false,
			TriggerID:   id,
			CronJob:     cronName,
			Namespace:   pluginNamespace,
			Message:     err.Error(),
			GeneratedAt: now,
		}, err
	}

	if len(cj.Status.Active) > 0 {
		return CronJobTriggerResponse{
			OK:          false,
			TriggerID:   id,
			CronJob:     cronName,
			Namespace:   pluginNamespace,
			Message:     "Job already running for this CronJob",
			GeneratedAt: now,
		}, fmt.Errorf("cronjob %s has active jobs", cronName)
	}

	jobName := fmt.Sprintf("%s-manual-%d", cronName, now.Unix())
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: jobName,
			Namespace: pluginNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":     "bifrost-research",
				"bifrost.dev/triggered-by":      "platform-api",
				"bifrost.dev/research-trigger":  id,
			},
		},
		Spec: cj.Spec.JobTemplate.Spec,
	}
	// Propagate owner labels from CronJob template when present.
	if cj.Spec.JobTemplate.Labels != nil {
		for k, v := range cj.Spec.JobTemplate.Labels {
			job.ObjectMeta.Labels[k] = v
		}
	}

	created, err := clientset.BatchV1().Jobs(pluginNamespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return CronJobTriggerResponse{
				OK:          false,
				TriggerID:   id,
				JobName:     jobName,
				CronJob:     cronName,
				Namespace:   pluginNamespace,
				Message:     "job name collision — retry",
				GeneratedAt: now,
			}, err
		}
		return CronJobTriggerResponse{
			OK:          false,
			TriggerID:   id,
			CronJob:     cronName,
			Namespace:   pluginNamespace,
			Message:     err.Error(),
			GeneratedAt: now,
		}, err
	}

	return CronJobTriggerResponse{
		OK:          true,
		JobName:     created.Name,
		CronJob:     cronName,
		Namespace:   pluginNamespace,
		StartedAt:   now,
		TriggerID:   id,
		Message:     "manual CronJob trigger accepted",
		GeneratedAt: now,
	}, nil
}

// CronJobTriggerCatalog returns whitelist entries for Console catalog.
func CronJobTriggerCatalog() []map[string]string {
	out := make([]map[string]string, 0, len(cronJobTriggers))
	for id, name := range cronJobTriggers {
		out = append(out, map[string]string{
			"trigger_id": id,
			"cronjob":    name,
			"namespace":  pluginNamespace,
		})
	}
	return out
}