package analytics

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

type Service struct {
	cfg     Config
	cluster *cluster.Service
	client  *http.Client
}

func NewService(clusterSvc *cluster.Service) *Service {
	return &Service{
		cfg:     ConfigFromEnv(),
		cluster: clusterSvc,
		client:  &http.Client{Timeout: proxyTimeoutSec * time.Second},
	}
}

type StatusResponse struct {
	Healthy         bool       `json:"healthy"`
	Reachable       bool       `json:"reachable"`
	ReportAvailable bool       `json:"report_available"`
	ReportBytes     int64      `json:"report_bytes,omitempty"`
	LastSchedule    *time.Time `json:"last_schedule,omitempty"`
	CronJobActive   int        `json:"cronjob_active"`
	DocsReady       int32      `json:"docs_ready"`
	DocsDesired     int32      `json:"docs_desired"`
	ModelsTotal     int        `json:"models_total"`
	Namespace       string     `json:"namespace"`
	Error           string     `json:"error,omitempty"`
	Hint            string     `json:"hint,omitempty"`
	GeneratedAt     time.Time  `json:"generated_at"`
}

func (s *Service) Status(ctx context.Context) StatusResponse {
	now := time.Now().UTC()
	resp := StatusResponse{
		ModelsTotal: 21,
		Namespace:   pluginNamespace,
		GeneratedAt: now,
	}

	clientset, err := s.kubeClient()
	if err != nil {
		resp.Error = err.Error()
		resp.Hint = "Set KUBECONFIG or run in-cluster; optionally ANALYTICS_DOCS_URL for docs probe"
		// Still try HTTP override for report
		s.probeReport(&resp)
		return resp
	}

	cj, err := clientset.BatchV1().CronJobs(pluginNamespace).Get(ctx, cronJobName, metav1.GetOptions{})
	if err != nil {
		resp.Error = "cronjob: " + err.Error()
		resp.Hint = "Apply bifrost-analytics CronJob in plugin-market-data NS"
	} else {
		resp.CronJobActive = len(cj.Status.Active)
		if cj.Status.LastScheduleTime != nil {
			t := cj.Status.LastScheduleTime.Time.UTC()
			resp.LastSchedule = &t
		}
	}

	deploy, err := clientset.AppsV1().Deployments(pluginNamespace).Get(ctx, docsDeployName, metav1.GetOptions{})
	if err != nil {
		if resp.Error == "" {
			resp.Error = "docs deploy: " + err.Error()
			resp.Hint = "Apply k8s/report-nginx.yaml (analytics-docs)"
		}
	} else {
		resp.DocsReady = deploy.Status.ReadyReplicas
		if deploy.Spec.Replicas != nil {
			resp.DocsDesired = *deploy.Spec.Replicas
		}
	}

	s.probeReport(&resp)

	resp.Reachable = resp.DocsReady > 0 || resp.ReportAvailable
	resp.Healthy = resp.DocsReady > 0 && resp.ReportAvailable && resp.Error == ""
	return resp
}

func (s *Service) probeReport(resp *StatusResponse) {
	httpReq, err := http.NewRequest(http.MethodGet, "http://unused/elementary_report.html", nil)
	if err != nil {
		return
	}
	httpReq.Header.Set("Range", "bytes=0-0")

	var r *http.Response
	if s.cfg.DocsBaseURL != "" {
		target := s.cfg.DocsBaseURL + "/elementary_report.html"
		req, reqErr := http.NewRequest(http.MethodGet, target, nil)
		if reqErr != nil {
			return
		}
		req.Header.Set("Range", "bytes=0-0")
		r, err = s.client.Do(req)
	} else {
		r, err = s.proxyViaKube(httpReq, "/elementary_report.html")
	}
	if err != nil {
		if resp.Error == "" {
			resp.Error = "report probe: " + err.Error()
		}
		return
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode == http.StatusOK || r.StatusCode == http.StatusPartialContent {
		resp.ReportAvailable = true
		// Range responses often set Content-Length=1; total size is in Content-Range: bytes 0-0/N
		if cr := r.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndex(cr, "/"); i >= 0 && i+1 < len(cr) {
				var n int64
				if _, scanErr := fmt.Sscan(cr[i+1:], &n); scanErr == nil && n > 0 {
					resp.ReportBytes = n
				}
			}
		}
		if resp.ReportBytes == 0 {
			if cl := r.Header.Get("Content-Length"); cl != "" {
				var n int64
				_, _ = fmt.Sscan(cl, &n)
				if n > 1 {
					resp.ReportBytes = n
				}
			} else if r.ContentLength > 1 {
				resp.ReportBytes = r.ContentLength
			}
		}
	}
}

func (s *Service) Proxy(r *http.Request, pluginPath string) (*http.Response, error) {
	pluginPath = "/" + strings.TrimPrefix(pluginPath, "/")
	if s.cfg.DocsBaseURL != "" {
		target := s.cfg.DocsBaseURL + pluginPath
		if q := r.URL.RawQuery; q != "" {
			target = target + "?" + q
		}
		return s.proxyHTTP(r, target, nil)
	}
	return s.proxyViaKube(r, pluginPath)
}

func (s *Service) proxyHTTP(r *http.Request, target string, transport http.RoundTripper) (*http.Response, error) {
	client := &http.Client{Timeout: proxyTimeoutSec * time.Second}
	if transport != nil {
		client.Transport = transport
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		return nil, err
	}
	for _, h := range []string{"Accept", "Accept-Encoding", "Range", "If-None-Match", "If-Modified-Since"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	return client.Do(req)
}

func (s *Service) proxyViaKube(r *http.Request, pluginPath string) (*http.Response, error) {
	if s.cluster == nil {
		return nil, fmt.Errorf("cluster service unavailable; set ANALYTICS_DOCS_URL for local override")
	}
	restCfg, _, err := s.cluster.RestConfig()
	if err != nil {
		return nil, err
	}

	rawPath := strings.TrimPrefix(pluginPath, "/")
	if q := r.URL.RawQuery; q != "" {
		rawPath = rawPath + "?" + q
	}

	svcProxy := fmt.Sprintf("%s:%s", docsServiceName, docsServicePort)
	host := strings.TrimRight(restCfg.Host, "/")
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	target, err := url.JoinPath(host, "api", "v1", "namespaces", pluginNamespace, "services", svcProxy, "proxy")
	if err != nil {
		return nil, err
	}
	target = strings.TrimRight(target, "/") + "/" + rawPath

	rt, err := rest.TransportFor(restCfg)
	if err != nil {
		return nil, err
	}
	return s.proxyHTTP(r, target, rt)
}

func (s *Service) kubeClient() (*kubernetes.Clientset, error) {
	if s.cluster == nil {
		return nil, fmt.Errorf("cluster service unavailable")
	}
	restCfg, _, err := s.cluster.RestConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restCfg)
}
