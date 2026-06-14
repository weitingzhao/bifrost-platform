import type { OpsContextDeployment } from '@/api/types'

export type CiModeLabel = 'Mac runner (near-term)' | 'GitOps (Gitea/Tekton/ArgoCD)'

export function ciModeLabel(phase: string): CiModeLabel {
  if (phase === 'k3s_partial' || phase === 'k3s_ha') {
    return 'GitOps (Gitea/Tekton/ArgoCD)'
  }
  return 'Mac runner (near-term)'
}

export function showGitOpsPlannedBadge(deployment: OpsContextDeployment): boolean {
  return deployment.phase === 'compose'
}

export const DELIVERY_STRATEGY_BULLETS = [
  'Self-hosted GitOps on K3s — Gitea, Tekton, ArgoCD (not GitHub Actions); strategy code stays on LAN.',
  'Near-term: Mac Mini #2 runs PR checks (pytest, npm build, check:legacy-css) before compose prod on mini-pc-a.',
  'Target: git push → Gitea webhook → Tekton build/test → internal Registry → ArgoCD sync to bifrost namespace.',
] as const
