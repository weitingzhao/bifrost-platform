package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const stgWindowGenerationPrefix = "stgrel-gen:"

// STGWindow is an immutable read/CAS token. Keep the actual ConfigMap private:
// renewing must use the held version, never refresh it after a conflict.
type STGWindow struct {
	Who, What, Env, ResourceVersion string
	Generation                      int64
	Expires                         time.Time
	cm                              *corev1.ConfigMap
}

func (w STGWindow) Live(at time.Time) bool { return w.Who != "" && at.Before(w.Expires) }

func decodeSTGWindow(cm *corev1.ConfigMap) (STGWindow, error) {
	r, ok := parseWindowCM(cm)
	expires, err := time.Parse(time.RFC3339, r.ExpiresAt)
	if !ok || err != nil || cm.ResourceVersion == "" {
		return STGWindow{}, errWindowUnreadable
	}
	var gen int64
	if strings.HasPrefix(r.Reason, stgWindowGenerationPrefix) {
		gen, err = strconv.ParseInt(strings.TrimPrefix(r.Reason, stgWindowGenerationPrefix), 10, 64)
		if err != nil || gen < 1 || r.Reason != stgWindowGenerationPrefix+strconv.FormatInt(gen, 10) {
			return STGWindow{}, errWindowUnreadable
		}
	}
	return STGWindow{Who: r.Who, What: r.What, Env: r.Env, ResourceVersion: cm.ResourceVersion, Generation: gen, Expires: expires, cm: cm.DeepCopy()}, nil
}

// ReadSTGWindow preserves expired records and their generation. It never
// deletes or repairs malformed records, including an unreadable expiry.
func (a *STGReleaseAdapter) ReadSTGWindow(ctx context.Context) (STGWindow, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	cs, err := a.core()
	if err != nil {
		return STGWindow{}, err
	}
	cm, err := cs.CoreV1().ConfigMaps(STGReleaseNamespace).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return STGWindow{}, nil
	}
	if err != nil {
		return STGWindow{}, err
	}
	return decodeSTGWindow(cm)
}

// TakeSTGWindow writes exactly the observed version once. Adoption of a live
// same-who window is only for a replayed outstanding started intent; the holder
// supplies recover after checking that intent. A conflict always means wait.
func (a *STGReleaseAdapter) TakeSTGWindow(ctx context.Context, old STGWindow, who string, generation int64, recover bool) (STGWindow, error) {
	if !windowWhoPattern.MatchString(who) || !strings.HasPrefix(who, "stgrel-bifrost-platform-") || generation <= old.Generation || generation < 1 {
		return STGWindow{}, fmt.Errorf("invalid STG window identity or generation")
	}
	if old.Live(a.now()) && (!recover || old.Who != who || old.What != "bifrost-platform" || old.Env != "stg" || old.Generation < 1) {
		return STGWindow{}, &WindowHeld{Who: old.Who, What: old.What}
	}
	now := a.now()
	r := windowRecord{Who: who, What: "bifrost-platform", Env: "stg", StartedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339), Reason: stgWindowGenerationPrefix + strconv.FormatInt(generation, 10)}
	if old.Live(now) {
		previous, _ := parseWindowCM(old.cm)
		r.StartedAt = previous.StartedAt
	}
	return a.writeSTGWindow(ctx, old, r)
}

func (a *STGReleaseAdapter) RenewSTGWindow(ctx context.Context, held STGWindow) (STGWindow, error) {
	if held.cm == nil || held.Generation < 1 || !held.Live(a.now()) {
		return STGWindow{}, &WindowChanged{}
	}
	r, _ := parseWindowCM(held.cm)
	r.ExpiresAt = a.now().Add(5 * time.Minute).Format(time.RFC3339)
	return a.writeSTGWindow(ctx, held, r)
}

// EndSTGWindow follows the existing release API's versioned expiry update,
// not a Delete. In particular it never GETs a newer version to release it.
func (a *STGReleaseAdapter) EndSTGWindow(ctx context.Context, held STGWindow) error {
	if held.cm == nil || held.Generation < 1 {
		return &WindowChanged{}
	}
	r, _ := parseWindowCM(held.cm)
	r.ExpiresAt = a.now().Format(time.RFC3339)
	_, err := a.writeSTGWindow(ctx, held, r)
	return err
}

func (a *STGReleaseAdapter) writeSTGWindow(ctx context.Context, old STGWindow, next windowRecord) (STGWindow, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	cs, err := a.core()
	if err != nil {
		return STGWindow{}, err
	}
	body, err := json.Marshal(next)
	if err != nil {
		return STGWindow{}, err
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: STGReleaseNamespace}}
	if old.cm != nil {
		cm = old.cm.DeepCopy()
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[releaseWindowDataKey] = string(body)
	cms := cs.CoreV1().ConfigMaps(STGReleaseNamespace)
	if old.cm == nil {
		cm, err = cms.Create(ctx, cm, metav1.CreateOptions{})
	} else {
		cm, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
	}
	if apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) || apierrors.IsNotFound(err) {
		return STGWindow{}, &WindowChanged{}
	}
	if err != nil {
		return STGWindow{}, err
	}
	return decodeSTGWindow(cm)
}
