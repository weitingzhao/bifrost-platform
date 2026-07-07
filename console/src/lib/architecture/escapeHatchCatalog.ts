/**
 * L0/L1 escape hatch runbook — Wave 4c (p6-escape-hatch).
 * Live probes: GET /api/v1/platform/escape-hatch
 */

export const ESCAPE_HATCH_RUNBOOK_VERSION = '2026-07-07.1'

export type EscapeHatchRunbookStep = {
  order: number
  title: string
  detail: string
}

export const ESCAPE_HATCH_RUNBOOK_STEPS: EscapeHatchRunbookStep[] = [
  {
    order: 1,
    title: 'Assess blast radius',
    detail: 'Confirm whether failure is L1 (platform-api/console) or L0 (K3s/Tekton/Argo). Use Control Room rocket/control signals + Self-health panel.',
  },
  {
    order: 2,
    title: 'Route A — Local make start',
    detail: 'On Mac with repo checkout: cd bifrost-platform && make start — bypasses cluster entirely (:8780 API, :5180 Console).',
  },
  {
    order: 3,
    title: 'Route B — Cluster NodePort',
    detail: 'If Deployments are healthy but ingress/DNS failed, use STG/PROD NodePort URLs from environments.yaml (same probes as self-health).',
  },
  {
    order: 4,
    title: 'Route C — kubectl overlay',
    detail: 'When pipeline/Argo is broken: kubectl apply -k k8s/overlays/platform-stg (or platform-prod) with valid PLATFORM_KUBECONFIG.',
  },
  {
    order: 5,
    title: 'Record quarterly drill',
    detail: 'After any escape-hatch exercise, record drill on Platform Release → Escape hatch panel (90-day schedule).',
  },
]

export const ESCAPE_HATCH_QUARTERLY_INTERVAL_DAYS = 90
