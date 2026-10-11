import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { fetchAuthCapabilities } from '@/api/cluster'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { forgetLegacyApprovalToken } from '@/lib/forgetLegacyApprovalToken'
import { PLATFORM_TOKEN_KEY, getPlatformOperatorToken, setPlatformOperatorToken } from '@/lib/platformAuth'
import {
  beginSignInEpoch,
  capabilityQueryKey,
  currentSignInEpoch,
  restoreSignInEpoch,
  type SignInEpoch,
} from '@/lib/signInEpoch'

export function PlatformAuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const [epoch, setEpoch] = useState(() => {
    forgetLegacyApprovalToken()
    return beginSignInEpoch(getPlatformOperatorToken())
  })
  const aligned = useRef(false)
  if (!aligned.current) {
    aligned.current = true
    restoreSignInEpoch(epoch)
  }

  const adopt = useCallback(
    (started: SignInEpoch) => {
      void qc.cancelQueries({ queryKey: ['platform', 'auth', 'capabilities'] })
      setEpoch(started)
    },
    [qc],
  )

  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key != null && event.key !== PLATFORM_TOKEN_KEY) return
      adopt(beginSignInEpoch(getPlatformOperatorToken()))
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [adopt])

  const capsQuery = useQuery({
    queryKey: capabilityQueryKey(epoch.id),
    queryFn: async ({ signal }) => {
      const id = epoch.id
      const token = epoch.token
      const caps = await fetchAuthCapabilities(token, signal)
      if (currentSignInEpoch().id !== id) {
        throw new Error('auth capabilities: sign-in changed')
      }
      return { epoch: id, caps }
    },
    refetchInterval: 60_000,
  })

  const invalidateActuation = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['platform', 'auth'] })
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
  }, [qc])

  const setToken = useCallback(
    (next: string) => {
      setPlatformOperatorToken(next)
      adopt(beginSignInEpoch(getPlatformOperatorToken()))
      invalidateActuation()
    },
    [adopt, invalidateActuation],
  )

  const signOut = useCallback(() => {
    setPlatformOperatorToken('')
    adopt(beginSignInEpoch(getPlatformOperatorToken()))
    invalidateActuation()
  }, [adopt, invalidateActuation])

  const refreshCapabilities = useCallback(() => {
    void qc.invalidateQueries({ queryKey: capabilityQueryKey(epoch.id) })
  }, [qc, epoch.id])

  const read = capsQuery.data
  const caps = read != null && read.epoch === epoch.id ? read.caps : undefined
  const value = useMemo<PlatformAuthContextValue>(
    () => ({
      token: epoch.token,
      epoch: epoch.id,
      caps,
      capsEpoch: caps != null ? epoch.id : null,
      capsLoading: capsQuery.isLoading,
      canOperate: caps?.can_operate === true,
      canAdmin: caps?.can_admin === true,
      setToken,
      signOut,
      refreshCapabilities,
    }),
    [epoch, caps, capsQuery.isLoading, setToken, signOut, refreshCapabilities],
  )

  return <PlatformAuthContext.Provider value={value}>{children}</PlatformAuthContext.Provider>
}
