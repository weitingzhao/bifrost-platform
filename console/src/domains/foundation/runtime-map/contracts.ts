import type { Reachability } from '@/api/matrixTypes'
import type { ScopeRow } from '@/lib/environments-catalog'

export type ScopeTag = ScopeRow['tag']

export type RoleView = 'compose' | 'k3s'

export type InfraComponentVisual = {
  id: string
  label: string
  lettermark: string
  brandColor: string
  simpleIconSlug?: string
  scopeTag?: ScopeTag
  matrixTargets?: string[]
  plannedInK3s?: boolean
  plannedInCompose?: boolean
}

export type StackChipModel = {
  chipId: string
  componentId: string
  label: string
  lettermark: string
  brandColor: string
  planned: boolean
  ghost?: boolean
  reachability: Reachability
  matrixTargetId?: string
  scopeTag?: ScopeTag
}

export type RuntimeMapSelection =
  | { kind: 'node'; id: string }
  | { kind: 'target'; id: string }
  | { kind: 'scope'; tag: ScopeTag }
  | { kind: 'edge'; id: string }
  | null
