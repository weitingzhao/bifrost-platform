// Package k8sstate stores statefile keys in ConfigMaps of the pod's own
// namespace: one ConfigMap per key, named platform-state-<key>, so STG and PROD
// never share state, a rollout keeps it, and the api and workers pods read the
// same copy (TD-196).
//
// Writes are last-writer-wins: a store reads, changes and writes under its own
// mutex, and two pods writing the same key at the same moment can lose one
// change. Platform state is written rarely (an operator action, a patrol run,
// an audit record), so this trade buys a simple store.
package k8sstate

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	dataKey = "state.json"
	// A ConfigMap holds at most 1 MiB; leave room for metadata.
	MaxBytes  = 900 * 1024
	labelKey  = "bifrost.io/platform-state"
	keyAnnot  = "bifrost.io/state-key"
	namePrefx = "platform-state-"
)

// Backend implements statefile.Backend.
type Backend struct {
	ns      string
	clients func() (kubernetes.Interface, error)
}

func New(namespace string, clients func() (kubernetes.Interface, error)) *Backend {
	return &Backend{ns: namespace, clients: clients}
}

var unsafe = regexp.MustCompile(`[^a-z0-9-]+`)

// Name is the ConfigMap that holds key: platform-state-checklist-signals-json.
func Name(key string) string {
	n := unsafe.ReplaceAllString(strings.ToLower(key), "-")
	n = strings.Trim(n, "-")
	if len(n) > 253-len(namePrefx) {
		n = n[len(n)-(253-len(namePrefx)):]
	}
	return namePrefx + n
}

func (b *Backend) Read(ctx context.Context, key string) ([]byte, error) {
	core, err := b.clients()
	if err != nil {
		return nil, err
	}
	cm, err := core.CoreV1().ConfigMaps(b.ns).Get(ctx, Name(key), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	data, ok := cm.Data[dataKey]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(data), nil
}

func (b *Backend) Write(ctx context.Context, key string, data []byte) error {
	if len(data) > MaxBytes {
		return fmt.Errorf("state %s is %d bytes, over the %d-byte ConfigMap budget", key, len(data), MaxBytes)
	}
	core, err := b.clients()
	if err != nil {
		return err
	}
	cms := core.CoreV1().ConfigMaps(b.ns)
	name := Name(key)
	for attempt := 0; attempt < 3; attempt++ {
		cm, err := cms.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = cms.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: name, Namespace: b.ns,
					Labels:      map[string]string{labelKey: "true", "app.kubernetes.io/managed-by": "bifrost-platform"},
					Annotations: map[string]string{keyAnnot: key},
				},
				Data: map[string]string{dataKey: string(data)},
			}, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return err
		}
		if err != nil {
			return err
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[dataKey] = string(data)
		_, err = cms.Update(ctx, cm, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			continue
		}
		return err
	}
	return fmt.Errorf("state %s: still conflicting after 3 attempts", key)
}

// Namespace is PLATFORM_STATE_NAMESPACE, else the pod's own namespace.
func Namespace() string {
	if ns := strings.TrimSpace(os.Getenv("PLATFORM_STATE_NAMESPACE")); ns != "" {
		return ns
	}
	raw, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// ClientsFromEnv builds a client from PLATFORM_KUBECONFIG (what the rest of
// platform-api uses), else from the pod's in-cluster service account. The
// client is built once, on first use.
func ClientsFromEnv() func() (kubernetes.Interface, error) {
	var (
		once sync.Once
		core kubernetes.Interface
		err  error
	)
	return func() (kubernetes.Interface, error) {
		once.Do(func() {
			var cfg *rest.Config
			if path := strings.TrimSpace(os.Getenv("PLATFORM_KUBECONFIG")); path != "" {
				cfg, err = clientcmd.BuildConfigFromFlags("", path)
			} else {
				cfg, err = rest.InClusterConfig()
			}
			if err == nil {
				core, err = kubernetes.NewForConfig(cfg)
			}
		})
		return core, err
	}
}

// Wanted: PLATFORM_STATE_BACKEND=configmap|file overrides; otherwise
// ConfigMaps in the cluster, files elsewhere.
func Wanted() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_STATE_BACKEND"))) {
	case "configmap":
		return true
	case "file":
		return false
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}
