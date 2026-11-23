// Package elasticreadings is the compute domain's reading of service
// readiness and of on-demand nodes. It decides; it does not fetch. It imports
// only foundation packages and does not import cluster: cluster gathers the
// nodes and Deployments and the server binds the three domain readings to it
// (cluster.BindComputeReadings). NodeMode and HostReadings are called by
// package computenodes (compute), which gathers a node's observations and is
// bound to cluster since 2026-11-19 (cluster.BindComputeNodes).
//
// Contract:
//
//   - GPUDomain and WorkersDomain take a
//     resourceview.ElasticSnapshot and return the GPU / AI and
//     general workers lines of GET /cluster/service-readiness.
//   - NodeMode takes a resourceview.ElasticNodeObservation and says whether an
//     on-demand node is active, standby or degraded, with why. It calls the
//     observation's functions only when it needs them, because the last is an
//     SSH probe of the host.
//   - HostReadings.Read answers whether a host is up for a page that polls,
//     from a kept reading up to two minutes old. Nothing may act on it.
//
// What the answers mean: a powered-off GPU or warehouse node is standby and ok
// while nothing asks for it, and missing compute once something does. General
// workers are standby when everything else is ready and the only gap is a
// trading daemon scaled to zero. The daemon is only observed here: nothing in
// this package starts, scales or stops anything.
//
// When cluster has no reader bound, the domains report unavailable, so an
// unbound reader never reads as ready or standby. When computenodes is not
// bound, no node is on demand: each counts as a core node.
//
// The domain is compute. The code is maintained by the foundation decoupling
// line until compute has a seat that writes code. The functions moved from
// cluster on 2026-11-16 (GPU, warehouse, NodeMode) and 2026-11-18 (general
// workers), unchanged.
package elasticreadings
