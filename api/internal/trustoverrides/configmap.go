// Package trustoverrides keeps the Owner's per-skill trust overrides in a
// ConfigMap in platform-api's namespace, so a grant survives rollouts and every
// replica reads the same one (TD-229). It lives outside agentgovernance because
// that package belongs to the operator plane, which must run with no cluster.
package trustoverrides

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
)

const (
	configMapName = "platform-trust-overrides"
	dataKey       = "overrides.json"
)

// NewConfigMapStore keeps the overrides in one ConfigMap in ns —
// the same get-or-create, retry-on-conflict pattern as internal/threadtitles —
// so they survive rollouts and every replica reads the same grant.
func NewConfigMapStore(ns string, clients func() (kubernetes.Interface, error)) agentgovernance.TrustOverrideStore {
	return &configMapTrustOverrides{ns: ns, clients: clients}
}

type configMapTrustOverrides struct {
	ns      string
	clients func() (kubernetes.Interface, error)
}

func (s *configMapTrustOverrides) Location() string {
	return "configmap " + s.ns + "/" + configMapName
}

func (s *configMapTrustOverrides) List(ctx context.Context) (map[string]agentgovernance.TrustOverride, error) {
	core, err := s.clients()
	if err != nil {
		return nil, fmt.Errorf("trust overrides: %w", err)
	}
	cm, err := core.CoreV1().ConfigMaps(s.ns).Get(ctx, configMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string]agentgovernance.TrustOverride{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.Location(), err)
	}
	return agentgovernance.DecodeTrustOverrides([]byte(cm.Data[dataKey]), s.Location())
}

func (s *configMapTrustOverrides) Put(ctx context.Context, o agentgovernance.TrustOverride) error {
	core, err := s.clients()
	if err != nil {
		return fmt.Errorf("trust overrides: %w", err)
	}
	o = agentgovernance.StampTrustOverride(o)
	for attempt := 0; attempt < 3; attempt++ {
		cm, err := core.CoreV1().ConfigMaps(s.ns).Get(ctx, configMapName, metav1.GetOptions{})
		missing := apierrors.IsNotFound(err)
		if err != nil && !missing {
			return fmt.Errorf("read %s: %w", s.Location(), err)
		}
		all := map[string]agentgovernance.TrustOverride{}
		if !missing {
			if all, err = agentgovernance.DecodeTrustOverrides([]byte(cm.Data[dataKey]), s.Location()); err != nil {
				return err
			}
		}
		all[o.SkillID] = o
		raw, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			return err
		}
		if missing {
			cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: s.ns,
				Labels: map[string]string{"app.kubernetes.io/managed-by": "bifrost-platform"}}}
		}
		cm.Data = map[string]string{dataKey: string(raw)}
		if missing {
			_, err = core.CoreV1().ConfigMaps(s.ns).Create(ctx, cm, metav1.CreateOptions{})
		} else {
			_, err = core.CoreV1().ConfigMaps(s.ns).Update(ctx, cm, metav1.UpdateOptions{})
		}
		if apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("write %s: %w", s.Location(), err)
		}
		return nil
	}
	return fmt.Errorf("write %s: still conflicting after 3 attempts", s.Location())
}
