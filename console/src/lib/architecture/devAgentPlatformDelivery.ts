/** Dev Agent Platform (DAP) — Phase delivery + sign-off state management. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const DAP_VERSION = '2026-07-04'

export interface DapDeliveryItem {
  id: 'DAP-0' | 'DAP-1' | 'DAP-2' | 'DAP-3' | 'DAP-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const DAP_DELIVERY_ITEMS: DapDeliveryItem[] = [
  {
    id: 'DAP-0',
    title: 'Blueprint YAML schema + TIBM externalization',
    summary:
      'Declarative program blueprint format. TIBM phases extracted from Go handler into config/programs/ YAML.',
    verifySteps: [
      'config/programs/_schema.yaml exists with full field documentation.',
      'config/programs/trade-ib-client-migration.yaml has 5 phases (TIBM0–4) with prompt_template + verify_cmd + acceptance.',
      'config/programs/example-template.yaml exists as copy-ready template for new programs.',
      'YAML is valid (parseable) and consistent with schema.',
    ],
  },
  {
    id: 'DAP-1',
    title: 'Go handler dynamic loader + multi-program API',
    summary:
      'Go handler loads blueprints from config/programs/*.yaml on startup. New API endpoints: GET /programs, POST /activate.',
    verifySteps: [
      'GET /api/v1/dev-agent/programs returns list of loaded programs.',
      'GET /api/v1/dev-agent/status includes program.id + program.title.',
      'Adding a new .yaml file and restarting API makes it appear in /programs.',
      'POST /api/v1/dev-agent/programs/{id}/activate switches active program.',
    ],
  },
  {
    id: 'DAP-2',
    title: 'State persistence (JSON file store)',
    summary:
      'Phase status + job history persisted to state/dev-agent/{program}.json. Process restart preserves state.',
    verifySteps: [
      'Start a phase → restart API → GET /status shows phase as "running" (or last known state).',
      'Approve a phase → restart API → phase shows "done".',
      'History entries survive restart.',
      'State file is human-readable JSON.',
    ],
  },
  {
    id: 'DAP-3',
    title: 'FE Program Selector + dynamic Phase Board',
    summary:
      'Console DevAgentPage has program selector. Phase Board, title, and acceptance criteria render from API.',
    verifySteps: [
      'Program dropdown/segment shows all loaded programs.',
      'Switching program updates Phase Board phases.',
      'Acceptance criteria visible per phase (from blueprint).',
      'Completed programs show "done" badge in selector.',
    ],
  },
  {
    id: 'DAP-4',
    title: 'Bridge prompt template + skill injection',
    summary:
      'bridge.ts receives prompt from Go handler (rendered from YAML template). No hardcoded prompts in bridge or handler.',
    verifySteps: [
      'bridge.ts accepts --workspace, --model, --prompt, --skill-path args.',
      'Go handler renders prompt_template with {{var}} substitution.',
      'Adding a new program YAML → Start Phase works without code changes.',
      'Cursor Agent receives skill context in prompt.',
    ],
  },
]

export interface DapItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface DapSignoffState {
  version: string
  items: Record<string, DapItemVerification>
  signedOffAt: string | null
}

const STORAGE_KEY = 'bifrost:dap:signoff'

export function defaultDapSignoffState(): DapSignoffState {
  return {
    version: DAP_VERSION,
    items: {},
    signedOffAt: null,
  }
}

export function loadDapSignoffState(): DapSignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultDapSignoffState()
    const parsed = JSON.parse(raw) as DapSignoffState
    if (parsed.version !== DAP_VERSION) return defaultDapSignoffState()
    const merged = defaultDapSignoffState()
    merged.items = parsed.items ?? {}
    merged.signedOffAt = parsed.signedOffAt
    return merged
  } catch {
    return defaultDapSignoffState()
  }
}

export function saveDapSignoffState(state: DapSignoffState): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
}

export function dapVerificationCount(state: DapSignoffState): {
  verified: number
  total: number
} {
  const total = DAP_DELIVERY_ITEMS.length
  const verified = DAP_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total }
}

export function allDapItemsVerified(state: DapSignoffState): boolean {
  return DAP_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function isDapPhaseSignedOff(phaseId: string): boolean {
  const state = loadDapSignoffState()
  return state.items[phaseId]?.verified === true
}

export function isDapProgramSignedOff(): boolean {
  return loadDapSignoffState().signedOffAt != null
}

export { notifyGovernanceSignoffChanged }
