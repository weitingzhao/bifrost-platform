// Package topology composes the generic observation projection with the
// tenant display adapter. Acquisition and authorization stay with the caller.
package topology

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/tenanttopology"
	"github.com/weitingzhao/bifrost-platform/api/internal/topologyview"
)

type ServiceView = topologyview.ServiceView
type NodeView = topologyview.NodeView
type EdgeView = topologyview.EdgeView
type Response = topologyview.Response
type Live = topologyview.Live

func Build(topo *config.TopologyFile, env config.Environment, matrix probe.MatrixResponse, live Live) Response {
	return topologyview.Build(topo, env, matrix, live, tenanttopology.Adapter())
}
