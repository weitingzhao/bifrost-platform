// Package k8sstatetest exercises the real client-go limiter and k8sstate
// backend without listening on sockets or contacting a cluster.
package k8sstatetest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// New uses the production QPS=5/Burst=10 defaults and deadlines. Calls are
// serialized by the test; the transport adds no delay, even during throttling.
func New(t *testing.T, key string, raw []byte) (*k8sstate.Backend, *Transport) {
	t.Helper()
	if len(raw) > k8sstate.MaxBytes {
		t.Fatal("fixture exceeds backend capacity")
	}
	name := k8sstate.Name(key)
	transport := &Transport{Business: name, Objects: map[string]corev1.ConfigMap{
		name: {ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", ResourceVersion: "1"}, Data: map[string]string{"state.json": string(raw)}},
	}}
	client, err := kubernetes.NewForConfig(&rest.Config{Host: "https://state.invalid", Transport: transport,
		ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	return k8sstate.New("test", func() (kubernetes.Interface, error) { return client, nil }), transport
}

type Transport struct {
	Objects      map[string]corev1.ConfigMap
	Requests     int
	FailBusiness int
	Business     string
	FailArchive  bool
}

func (m *Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	m.Requests++
	name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if m.FailArchive && (strings.Contains(name, "-delivery-") || strings.Contains(name, "-walk-") || strings.HasSuffix(name, "-index")) {
		return nil, fmt.Errorf("archive unavailable")
	}
	if m.FailBusiness > 0 && name == m.Business && r.Method == http.MethodPut {
		m.FailBusiness--
		return nil, fmt.Errorf("business CAS reply lost")
	}
	status := http.StatusOK
	var result any
	switch r.Method {
	case http.MethodGet:
		if cm, ok := m.Objects[name]; ok {
			result = cm
		} else {
			status = http.StatusNotFound
			result = metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: "Failure", Reason: metav1.StatusReasonNotFound, Code: 404}
		}
	case http.MethodPost, http.MethodPut:
		var cm corev1.ConfigMap
		if err := json.NewDecoder(r.Body).Decode(&cm); err != nil {
			return nil, err
		}
		cm.ResourceVersion = fmt.Sprint(m.Requests)
		m.Objects[cm.Name] = cm
		result = cm
	default:
		return nil, fmt.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
}
