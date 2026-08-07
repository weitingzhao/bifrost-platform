package marketdata

import (
	"context"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// poolForCronJob maps a CronJob name (minus "market-data-" prefix) to its worker pool.
func poolForCronJob(name string) string {
	n := strings.TrimPrefix(name, "market-data-")
	switch {
	case strings.HasPrefix(n, "option"),
		strings.HasPrefix(n, "max-pain"),
		strings.HasPrefix(n, "atm-iv"),
		strings.HasPrefix(n, "iv-percentile"),
		strings.HasPrefix(n, "oi-gap"):
		return "options"
	case strings.HasPrefix(n, "stock"),
		strings.HasPrefix(n, "reference"),
		strings.HasPrefix(n, "calendar"),
		strings.HasPrefix(n, "corporate"),
		strings.HasPrefix(n, "fundamentals"),
		strings.HasPrefix(n, "universe"),
		strings.HasPrefix(n, "eod-pipeline"),
		strings.HasPrefix(n, "minute-bars"):
		return "stocks"
	default:
		return ""
	}
}

// probeCronJobNextFire returns the earliest next fire time per pool by listing
// CronJobs in the plugin namespace and computing next fire from their schedules.
func (s *Service) probeCronJobNextFire(ctx context.Context, now time.Time) map[string]time.Time {
	result := make(map[string]time.Time)
	if s.cluster == nil {
		return result
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return result
	}
	list, err := clientset.BatchV1().CronJobs(pluginNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return result
	}
	for _, cj := range list.Items {
		if cj.Spec.Suspend != nil && *cj.Spec.Suspend {
			continue
		}
		pool := poolForCronJob(cj.Name)
		if pool == "" {
			continue
		}
		next := nextFireSimple(cj.Spec.Schedule, now)
		if next.IsZero() {
			continue
		}
		if existing, ok := result[pool]; !ok || next.Before(existing) {
			result[pool] = next
		}
	}
	return result
}

// nextFireSimple computes the next fire time for simple 5-field cron expressions.
// Supports: "MIN HOUR * * *" (daily), "MIN */N * * *" (every N hours),
// "MIN HOUR * * DOW" (weekly on specific day).
func nextFireSimple(schedule string, after time.Time) time.Time {
	schedule = strings.TrimSpace(schedule)
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return time.Time{}
	}
	minField := fields[0]
	hourField := fields[1]
	dowField := fields[4]

	minute, err := strconv.Atoi(minField)
	if err != nil || minute < 0 || minute > 59 {
		return time.Time{}
	}

	if strings.HasPrefix(hourField, "*/") {
		interval, err := strconv.Atoi(strings.TrimPrefix(hourField, "*/"))
		if err != nil || interval <= 0 {
			return time.Time{}
		}
		candidate := time.Date(after.Year(), after.Month(), after.Day(), 0, minute, 0, 0, time.UTC)
		for candidate.Before(after) || candidate.Equal(after) {
			candidate = candidate.Add(time.Duration(interval) * time.Hour)
		}
		return candidate
	}

	hour, err := strconv.Atoi(hourField)
	if err != nil || hour < 0 || hour > 23 {
		return time.Time{}
	}

	if dowField != "*" {
		dow, err := strconv.Atoi(dowField)
		if err != nil || dow < 0 || dow > 6 {
			return time.Time{}
		}
		candidate := time.Date(after.Year(), after.Month(), after.Day(), hour, minute, 0, 0, time.UTC)
		for candidate.Weekday() != time.Weekday(dow) || !candidate.After(after) {
			candidate = candidate.Add(24 * time.Hour)
		}
		return candidate
	}

	candidate := time.Date(after.Year(), after.Month(), after.Day(), hour, minute, 0, 0, time.UTC)
	if !candidate.After(after) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}
