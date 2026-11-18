import { tenantRouteLabel } from '@/domains/tenant/observability/routeProfile'
export function routeLabel(route: string): string {
  const tenant = tenantRouteLabel(route)
  if (tenant != null) return tenant
  switch (route) {
    case 'cluster':
      return 'Rocket → Cluster'
    case 'queue':
    case 'agent-desk':
      return 'Engineer → Queue'
    case 'operator-plane':
      return 'Engineer → Operator Plane'
    case 'network':
      return 'Ground → Network'
    case 'control-room':
      return 'Mission Control → Control Room'
    case 'blueprint':
      return 'Governance → Blueprint'
    case 'observability':
      return 'Mission Control → Observability'
    default:
      return route
  }
}
