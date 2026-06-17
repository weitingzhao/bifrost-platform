import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { fetchAuthCapabilities } from '@/api/platform'
import type { AuthCapabilities } from '@/api/types'
import { getPlatformOperatorToken, setPlatformOperatorToken } from '@/lib/platformAuth'

export interface PlatformAuthContextValue {
  token: string
  caps: AuthCapabilities | undefined
  capsLoading: boolean
  canOperate: boolean
  canAdmin: boolean
  setToken: (token: string) => void
  signOut: () => void
  refreshCapabilities: () => void
}

const PlatformAuthContext = createContext<PlatformAuthContextValue | null>(null)

export function PlatformAuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const [token, setTokenState] = useState(() => getPlatformOperatorToken())

  const capsQuery = useQuery({
    queryKey: ['platform', 'auth', 'capabilities', token],
    queryFn: fetchAuthCapabilities,
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
      setTokenState(getPlatformOperatorToken())
      invalidateActuation()
    },
    [invalidateActuation],
  )

  const signOut = useCallback(() => {
    setPlatformOperatorToken('')
    setTokenState('')
    invalidateActuation()
  }, [invalidateActuation])

  const refreshCapabilities = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['platform', 'auth', 'capabilities', token] })
  }, [qc, token])

  const value = useMemo<PlatformAuthContextValue>(
    () => ({
      token,
      caps: capsQuery.data,
      capsLoading: capsQuery.isLoading,
      canOperate: capsQuery.data?.can_operate === true,
      canAdmin: capsQuery.data?.can_admin === true,
      setToken,
      signOut,
      refreshCapabilities,
    }),
    [token, capsQuery.data, capsQuery.isLoading, setToken, signOut, refreshCapabilities],
  )

  return <PlatformAuthContext.Provider value={value}>{children}</PlatformAuthContext.Provider>
}

export function usePlatformAuth(): PlatformAuthContextValue {
  const ctx = useContext(PlatformAuthContext)
  if (ctx == null) {
    throw new Error('usePlatformAuth must be used within PlatformAuthProvider')
  }
  return ctx
}
