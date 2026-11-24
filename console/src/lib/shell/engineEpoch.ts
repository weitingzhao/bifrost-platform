import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { onEngineEpochChange } from '@/api/engine'

/**
 * The shell's half of the engine identity rule (engine-backend.md §4, 箭10 2026-11-24): when an engine answer comes
 * back under another epoch (another token, or the engine restarted), every engine read held under the old one is
 * dropped, so no view shows the old identity's data next to the new. Mounted once, in the shell.
 */
export function useEngineEpochReset(): void {
  const client = useQueryClient()
  useEffect(() => onEngineEpochChange(() => { client.removeQueries({ queryKey: ['engine'] }) }), [client])
}
