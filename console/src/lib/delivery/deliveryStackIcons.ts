/** Maps Delivery page node / stack add-on ids → Runtime Map component ids (simple-icons). */
const DELIVERY_COMPONENT_BY_ID: Record<string, string> = {
  gitea: 'gitea',
  tekton: 'tekton',
  registry: 'registry',
  argocd: 'argocd',
  'k3s-bifrost': 'k3s',
  'compose-prod-70': 'docker',
}

export function deliveryComponentId(id: string): string | undefined {
  return DELIVERY_COMPONENT_BY_ID[id]
}

export function hasDeliveryBrandIcon(id: string): boolean {
  return deliveryComponentId(id) != null
}
