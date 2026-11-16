// Package datahusbandry preserves existing wiring to the Tenant husbandry implementation.
package datahusbandry

import (
	tenant "github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry"
	"net/http"
)

type Snapshot = tenant.Snapshot
type LaneView = tenant.LaneView
type Service = tenant.Service
type Handler = tenant.Handler

type proxyer interface {
	Proxy(*http.Request, string) (*http.Response, error)
}

func NewService(market, flex, research proxyer) *Service {
	return tenant.NewService(market, flex, research)
}
func NewHandler(svc *Service) *Handler { return tenant.NewHandler(svc) }
