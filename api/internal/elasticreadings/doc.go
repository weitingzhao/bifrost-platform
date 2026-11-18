// Package elasticreadings is the compute domain's reading of service
// readiness and of on-demand nodes. It decides; it does not fetch. It imports
// only foundation packages and does not import cluster: cluster gathers the
// nodes and Deployments and the server binds these functions to it
// (cluster.BindComputeReadings).
//
// Contract:
//
//   - GPUDomain, WarehouseDomain and WorkersDomain take a
//     resourceview.ElasticSnapshot and return the GPU / AI, data warehouse and
//     general workers lines of GET /cluster/service-readiness.
//   - NodeMode takes a resourceview.ElasticNodeObservation and says whether an
//     on-demand node is active, standby or degraded, with why. It calls the
//     observation's functions only when it needs them, because the last is an
//     SSH probe of the host.
//
// What the answers mean: a powered-off GPU or warehouse node is standby and ok
// while nothing asks for it, and missing compute once something does. General
// workers are standby when everything else is ready and the only gap is a
// trading daemon scaled to zero. The daemon is only observed here: nothing in
// this package starts, scales or stops anything.
//
// When cluster has no reader bound, the domains report unavailable and on-demand
// nodes report degraded, so an unbound reader never reads as ready or standby.
//
// The domain is compute. The code is maintained by the foundation decoupling
// line until compute has a seat that writes code. The functions moved from
// cluster on 2026-11-16 (GPU, warehouse, NodeMode) and 2026-11-18 (general
// workers), unchanged.
package elasticreadings
