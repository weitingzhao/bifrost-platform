export function tenantRouteLabel(route: string): string | undefined {
  switch (route) {
    case 'satellite-health':
    case 'satellite-telemetry':
    case 'satellite-api':
      return 'Satellite → Satellite Health'
    case 'satellite-bus':
      return 'Satellite → Bus Status'
    case 'plugin-gallery':
      return 'Subcontractors → Plugin Gallery'
    default: return undefined
  }
}
