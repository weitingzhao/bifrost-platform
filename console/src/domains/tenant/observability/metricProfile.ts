export const TENANT_METRIC_TO_SIGNAL: Record<string, string> = {
  api_request_rate: 'satellite.api-request-rate',
  api_latency_p99: 'satellite.api-latency-p99',
  api_error_rate: 'satellite.api-error-rate',
  redis_memory_bytes: 'ground.redis-ib',
  redis_connected_clients: 'ground.redis-ib',
  ib_gateway_up: 'subcontractors.ib-gateway',
}
