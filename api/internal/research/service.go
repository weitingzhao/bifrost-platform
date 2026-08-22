package research

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

// Status probes Research API GET /health (HTTP override or kube service proxy).
func (s *Service) Status(ctx context.Context) StatusResponse {
	now := time.Now().UTC()
	resp := StatusResponse{
		Reachable:   false,
		GeneratedAt: now,
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unused"+healthPath, nil)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	var r *http.Response
	if base := strings.TrimRight(strings.TrimSpace(s.cfg.APIBaseURL), "/"); base != "" {
		target := base + healthPath
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if reqErr != nil {
			resp.Error = reqErr.Error()
			return resp
		}
		r, err = s.client.Do(req)
	} else {
		r, err = s.proxyViaKube(httpReq, healthPath)
	}
	if err != nil {
		resp.Error = err.Error()
		resp.Hint = "Ensure research-api is Running in research NS, or set RESEARCH_API_URL (e.g. http://127.0.0.1:8795)"
		return resp
	}
	defer func() { _ = r.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 64*1024))

	if r.StatusCode != http.StatusOK {
		resp.Error = fmt.Sprintf("HTTP %d", r.StatusCode)
		resp.Hint = "Research API /health did not return 200"
		return resp
	}
	resp.Reachable = true
	return resp
}
