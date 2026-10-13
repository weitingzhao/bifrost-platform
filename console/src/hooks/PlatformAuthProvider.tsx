import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { clearLegacyApprovalToken } from '@/api/approvals'
import { fetchAuthCapabilities } from '@/api/cluster'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { getPlatformOperatorToken, setPlatformOperatorToken } from '@/lib/platformAuth'

export function PlatformAuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const [token, setTokenState] = useState(() => getPlatformOperatorToken())

  useEffect(() => {
    clearLegacyApprovalToken()
  }, [])

  const capsQuery = useQuery({
    // The role belongs to the sign-in that asked for it: the request uses the token in the key, never whatever is
    // in storage when it runs, and the result is cached under that same token.
    queryKey: ['platform', 'auth', 'capabilities', token],
    queryFn: ({ queryKey, signal }) => fetchAuthCapabilities(queryKey[3] as string, signal),
    retry: false,
    refetchInterval: 60_000,
  })

  const invalidateActuation = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    // Cancel GET observers before invalidating: an initial read without data can
    // otherwise keep the old in-flight request instead of using the new login.
    for (const queryKey of [['approvals'], ['questions'], ['agent-progress'], ['agent-threads'], ['lineage']] as const) {
      void qc.cancelQueries({ queryKey }).then(() => qc.invalidateQueries({ queryKey }))
    }
  }, [qc])

  // Every role result is dropped, for every sign-in: a request still in flight is cancelled, so it cannot fill the
  // cache late, and going back to an earlier sign-in reads its role again instead of finding the old answer.
  const discardCapabilities = useCallback(() => {
    void qc.cancelQueries({ queryKey: ['platform', 'auth', 'capabilities'] })
    qc.removeQueries({ queryKey: ['platform', 'auth', 'capabilities'] })
  }, [qc])

  const setToken = useCallback(
    (next: string) => {
      discardCapabilities()
      setPlatformOperatorToken(next)
      setTokenState(getPlatformOperatorToken())
      invalidateActuation()
    },
    [discardCapabilities, invalidateActuation],
  )

  const signOut = useCallback(() => {
    discardCapabilities()
    setPlatformOperatorToken('')
    setTokenState('')
    invalidateActuation()
  }, [discardCapabilities, invalidateActuation])

  // After the server refused a decision the role on the page is not to be trusted: reset it (no data, so nothing
  // can be decided) and read it again now. A request that hangs is cancelled first, so this always sends a new one.
  const refreshCapabilities = useCallback(() => {
    void qc.cancelQueries({ queryKey: ['platform', 'auth', 'capabilities'] })
      .then(() => qc.resetQueries({ queryKey: ['platform', 'auth', 'capabilities'] }))
  }, [qc])

  const value = useMemo<PlatformAuthContextValue>(
    () => ({
      token,
      // A routine re-read every minute keeps the last answer while it runs (the buttons must not blink off once a
      // minute); a read that failed, or a role that was reset and is not back yet, leaves nothing to decide with.
      caps: capsQuery.isError ? undefined : capsQuery.data,
      capsLoading: capsQuery.isPending,
      capsError: capsQuery.isError,
      canOperate: !capsQuery.isError && capsQuery.data?.can_operate === true,
      canAdmin: !capsQuery.isError && capsQuery.data?.can_admin === true,
      setToken,
      signOut,
      refreshCapabilities,
    }),
    [token, capsQuery.data, capsQuery.isPending, capsQuery.isError, setToken, signOut, refreshCapabilities],
  )

  return <PlatformAuthContext.Provider value={value}>{children}</PlatformAuthContext.Provider>
}
