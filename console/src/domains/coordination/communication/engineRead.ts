import { exchangesOf, membersOf, type CommunicationFact, type CommunicationSnapshot, type Exchange, type Member } from '@bifrost/ui/communication'
import { engineQueryKey, engineUnreachableText, readEngine, type EngineAnswer } from '@/api/engine'

/**
 * Agents reading the engine (engine backend step 5, 箭10 2026-11-24): the communication snapshot comes from
 * GET /engine/v1/communication instead of the Console's own reads of the platform. The Console's direct read stays
 * as the way back, chosen by environment below; every environment starts off. An environment is switched on only
 * after the engine runs there and the comparison (exchanges, members, each stage's evidence) came out equal.
 *
 * The switch is keyed by the environment the platform says this Console views (/health viewer_env), so one image
 * behaves per environment and turning one on is a change here (default, 箭10 may veto).
 */
// STG on 2026-11-24 (箭10): the engine runs there; the comparison against the direct read is done on STG. PROD stays off
// until the engine is deployed there and STG compared equal.
export const ENGINE_COMMUNICATION: Record<string, boolean> = { dev: false, stg: true, prod: false }

/** Whether this environment reads communication from the engine; an environment not known yet reads directly. */
export const engineCommunicationOn = (viewerEnv: string | null | undefined): boolean => {
  const env = (viewerEnv ?? '').trim().toLowerCase()
  return env !== '' && Object.hasOwn(ENGINE_COMMUNICATION, env) && ENGINE_COMMUNICATION[env] === true
}

export const ENGINE_COMMUNICATION_PATH = '/engine/v1/communication'

/** The engine's data for this read (engine-server/src/communication.ts CommunicationRead, the fields Agents uses). */
export type EngineCommunication = {
  exchanges: Exchange[]
  members: Member[]
  coverage: { state: 'ready' | 'error'; note: string; updatedAt: string; truncated: boolean; history: { hasOlder: boolean; note: string } }
}
/** The query key of this read: under ['engine', …], so the shell drops it when the viewer's identity changes. */
export const ENGINE_COMMUNICATION_KEY = engineQueryKey(ENGINE_COMMUNICATION_PATH)

/**
 * The read's data, or why there is none: the engine not reached (engineUnreachableText, never a quiet fall back to
 * the platform), or reached with no data (the evidence says which platform read failed). Never said as empty.
 */
export function engineCommunicationOf(a: EngineAnswer<EngineCommunication>): EngineCommunication {
  if (!a.reached) throw new Error(engineUnreachableText(a))
  const d = a.data
  if (!d) {
    const why = a.evidence.filter(e => e.outcome !== 'read').map(e => e.source + (e.detail ? ': ' + e.detail : '')).join('; ')
    throw new Error('engine communication: no data' + (why ? ' (' + why + ')' : ''))
  }
  if (!Array.isArray(d.exchanges) || !Array.isArray(d.members) || !d.coverage) throw new Error('engine communication: data is not a communication read')
  return d
}

export async function fetchEngineCommunication(signal?: AbortSignal): Promise<EngineCommunication> {
  return engineCommunicationOf(await readEngine<EngineCommunication>(ENGINE_COMMUNICATION_PATH, signal ? { signal } : {}))
}

/**
 * The snapshot from the engine's read. Event deliveries are not in it yet (the engine serves them on
 * /engine/v1/traffic); until Agents reads that too, the bus facts the Console already streams are added here, so
 * the event channel is the same in both modes.
 */
export function engineSnapshot(read: EngineCommunication | undefined, error: unknown, busFacts: readonly CommunicationFact[], busNote: string): CommunicationSnapshot {
  const events = exchangesOf(busFacts)
  const exchanges = [...(read?.exchanges ?? []), ...events]
  return {
    version: 1,
    members: membersOf(exchanges, read?.members ?? []),
    exchanges,
    coverage: {
      state: error ? 'error' : read ? (read.coverage.state === 'error' ? 'error' : 'ready') : 'loading',
      updatedAt: read?.coverage.updatedAt,
      truncated: read?.coverage.truncated ?? false,
      note: (error ? String(error) + '. ' : '') + 'Read through the engine (/engine/v1/communication). ' + (read?.coverage.note ?? '') + ' ' + busNote,
    },
  }
}
