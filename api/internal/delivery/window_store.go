package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	windowTTLDefault = 5
	windowTTLMax     = 60
)

var (
	windowWhatPattern = regexp.MustCompile(`^[a-z0-9-]+(,[a-z0-9-]+)*$`)
	windowWhoPattern  = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,128}$`)
)

// WindowHeld is returned when another who already holds a live window.
type WindowHeld struct {
	Who  string
	What string
}

func (e *WindowHeld) Error() string {
	return fmt.Sprintf("release window held by %s (what=%s)", e.Who, e.What)
}

// WindowNotHolder is returned when a non-holder tries to release the window.
type WindowNotHolder struct {
	Who string
}

func (e *WindowNotHolder) Error() string {
	return fmt.Sprintf("only the holder can release the window (who=%s)", e.Who)
}

type windowRecord struct {
	Who       string `json:"who"`
	What      string `json:"what"`
	Env       string `json:"env"`
	PID       int    `json:"pid"`
	Host      string `json:"host"`
	StartedAt string `json:"started_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type windowInput struct {
	What       string
	Who        string
	Reason     string
	Env        string
	TTLMinutes int
}

func (r windowRecord) asMap() map[string]any {
	raw, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func (r windowRecord) live(now time.Time) bool {
	return r.Who != "" && !releaseWindowExpired(r.asMap(), now)
}

func validateWindowInput(in windowInput) (windowInput, error) {
	in.What = strings.TrimSpace(in.What)
	in.Who = strings.TrimSpace(in.Who)
	in.Reason = strings.TrimSpace(in.Reason)
	if !windowWhatPattern.MatchString(in.What) {
		return in, fmt.Errorf("what must be comma-separated repo names")
	}
	if !windowWhoPattern.MatchString(in.Who) {
		return in, fmt.Errorf("who has characters outside the allow-list")
	}
	if len(in.Reason) > 256 || strings.ContainsAny(in.Reason, "\n\r") {
		return in, fmt.Errorf("reason must not contain newlines and must be at most 256 characters")
	}
	in.Env = windowEnv(in.Env)
	if in.TTLMinutes == 0 {
		in.TTLMinutes = windowTTLDefault
	}
	if in.TTLMinutes < 1 || in.TTLMinutes > windowTTLMax {
		return in, fmt.Errorf("ttl_minutes must be between 1 and %d", windowTTLMax)
	}
	return in, nil
}

func windowEnv(reason string) string {
	switch reason {
	case "hold", "dev", "stg", "prod":
		return reason
	default:
		return "hold"
	}
}

func putReleaseWindow(ctx context.Context, cs kubernetes.Interface, ns string, in windowInput, now time.Time) (windowRecord, error) {
	in, err := validateWindowInput(in)
	if err != nil {
		return windowRecord{}, err
	}
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		rec, err := writeReleaseWindowOnce(ctx, cs, ns, in, now)
		if err == nil {
			return rec, nil
		}
		if _, held := err.(*WindowHeld); held {
			return windowRecord{}, err
		}
		if apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) {
			last = err
			continue
		}
		return windowRecord{}, err
	}
	if last == nil {
		last = fmt.Errorf("release window busy")
	}
	return windowRecord{}, last
}

func writeReleaseWindowOnce(ctx context.Context, cs kubernetes.Interface, ns string, in windowInput, now time.Time) (windowRecord, error) {
	cms := cs.CoreV1().ConfigMaps(ns)
	cm, err := cms.Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	missing := apierrors.IsNotFound(err)
	if err != nil && !missing {
		return windowRecord{}, err
	}
	var current windowRecord
	if !missing {
		current, _ = parseWindowCM(cm)
	}
	next := windowRecord{
		Who:       in.Who,
		What:      in.What,
		Env:       in.Env,
		PID:       0,
		Host:      "",
		StartedAt: now.UTC().Format(time.RFC3339),
		ExpiresAt: now.UTC().Add(time.Duration(in.TTLMinutes) * time.Minute).Format(time.RFC3339),
		Reason:    in.Reason,
	}
	if current.live(now) {
		if current.Who != in.Who {
			return windowRecord{}, &WindowHeld{Who: current.Who, What: current.What}
		}
		next.StartedAt = current.StartedAt
		if next.StartedAt == "" {
			next.StartedAt = now.UTC().Format(time.RFC3339)
		}
	}
	body, err := json.Marshal(next)
	if err != nil {
		return windowRecord{}, err
	}
	if missing {
		_, err = cms.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: ns},
			Data:       map[string]string{releaseWindowDataKey: string(body)},
		}, metav1.CreateOptions{})
		return next, err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[releaseWindowDataKey] = string(body)
	_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
	return next, err
}

func getReleaseWindow(ctx context.Context, cs kubernetes.Interface, ns string, now time.Time) (windowRecord, bool, error) {
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return windowRecord{}, false, nil
	}
	if err != nil {
		return windowRecord{}, false, err
	}
	rec, ok := parseWindowCM(cm)
	if !ok || !rec.live(now) {
		_ = cs.CoreV1().ConfigMaps(ns).Delete(ctx, releaseWindowConfigMap, metav1.DeleteOptions{})
		return windowRecord{}, false, nil
	}
	return rec, true, nil
}

func deleteReleaseWindow(ctx context.Context, cs kubernetes.Interface, ns, who string, force bool, now time.Time) error {
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	rec, ok := parseWindowCM(cm)
	if ok && rec.live(now) && !force && rec.Who != strings.TrimSpace(who) {
		return &WindowNotHolder{Who: rec.Who}
	}
	err = cs.CoreV1().ConfigMaps(ns).Delete(ctx, releaseWindowConfigMap, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func parseWindowCM(cm *corev1.ConfigMap) (windowRecord, bool) {
	if cm == nil || cm.Data == nil {
		return windowRecord{}, false
	}
	raw := strings.TrimSpace(cm.Data[releaseWindowDataKey])
	if raw == "" {
		return windowRecord{}, false
	}
	var rec windowRecord
	if json.Unmarshal([]byte(raw), &rec) != nil || strings.TrimSpace(rec.Who) == "" {
		return windowRecord{}, false
	}
	return rec, true
}
