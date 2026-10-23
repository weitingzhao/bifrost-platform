import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { clearLegacyApprovalToken } from '@/api/approvals'
import { fetchAuthCapabilities } from '@/api/cluster'
import { ISSUE_QUERY_KEY } from '@/api/issues'
import { CAPABILITIES_QUERY_KEY, PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { getPlatformOperatorToken, setPlatformOperatorToken } from '@/lib/platformAuth'

/** The routine role re-read. */
const ROLE_REREAD_MS = 60_000
/** After a role read failed the page says so; the next read comes sooner than the routine one. */
const ROLE_RETRY_MS = 10_000

export function PlatformAuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const [token, setTokenState] = useState(() => getPlatformOperatorToken())
  // The sign-in saved last. It differs from `token` only between a change of sign-in and the render that follows it.
  const savedToken = useRef(token)

  useEffect(() => {
    clearLegacyApprovalToken()
  }, [])

  const capsQuery = useQuery({
    // The role belongs to the sign-in that asked for it: the request uses the token in the key, never whatever is
    // in storage when it runs, and the result is cached under that same token.
    queryKey: [...CAPABILITIES_QUERY_KEY, token],
    queryFn: ({ queryKey, signal }) => fetchAuthCapabilities(queryKey[3] as string, signal),
    retry: false,
    refetchInterval: query => (query.state.status === 'error' ? ROLE_RETRY_MS : ROLE_REREAD_MS),
  })
  const readRoleNow = capsQuery.refetch

  const invalidateActuation = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    // Cancel GET observers before invalidating: an initial read without data can
    // otherwise keep the old in-flight request instead of using the new login. The
    // runtime's progress (its cards for the Owner, the plan's counts, the architecture
    // table) replaces the retired board's here (2.5 ⑤).
    for (const queryKey of [['approvals'], ['questions'], ['agent-runtime-progress'], ['agent-threads'], ['lineage']] as const) {
      void qc.cancelQueries({ queryKey }).then(() => qc.invalidateQueries({ queryKey }))
    }
    // A business issue's text is its readers' alone: every read of it goes with the sign-in it was made by, a request
    // still on its way cancelled (review 524 A1).
    void qc.cancelQueries({ queryKey: ISSUE_QUERY_KEY })
    qc.removeQueries({ queryKey: ISSUE_QUERY_KEY })
  }, [qc])

  // Every role result is dropped, for every sign-in: a request still in flight is cancelled, so it cannot fill the
  // cache late, and going back to an earlier sign-in reads its role again instead of finding the old answer.
  // Only for a change of sign-in: the render that follows reads the role of the new one.
  const discardCapabilities = useCallback(() => {
    void qc.cancelQueries({ queryKey: CAPABILITIES_QUERY_KEY })
    qc.removeQueries({ queryKey: CAPABILITIES_QUERY_KEY })
  }, [qc])

  // The role on the page is not to be trusted (the server refused a decision, or the same sign-in was saved again):
  // reset it (no data, so nothing can be decided) and read it again now. A request that hangs is cancelled first, so
  // this always sends a new one.
  const refreshCapabilities = useCallback(() => {
    void qc.cancelQueries({ queryKey: CAPABILITIES_QUERY_KEY }).then(() => {
      void qc.resetQueries({ queryKey: CAPABILITIES_QUERY_KEY })
      // resetQueries reads again only through a query this provider is still watching. If that query was removed
      // under it (a sign-out while signed out, a cleared cache), reading through the observer puts it back and sends
      // the read; when the reset already started one, this joins it and sends nothing more. Skipped while a change
      // of sign-in is still to be rendered: that render reads the new role itself.
      if (savedToken.current === token) void readRoleNow({ cancelRefetch: false })
    })
  }, [qc, readRoleNow, token])

  const setToken = useCallback(
    (next: string) => {
      setPlatformOperatorToken(next)
      const saved = getPlatformOperatorToken()
      savedToken.current = saved
      if (saved === token) {
        // The same sign-in saved again. Its role query is the one the page is watching, so it is not removed (the
        // page would keep the removed answer and nothing would read it again): it is reset and read again now.
        refreshCapabilities()
      } else {
        discardCapabilities()
      }
      setTokenState(saved)
      invalidateActuation()
    },
    [token, discardCapabilities, refreshCapabilities, invalidateActuation],
  )

  const signOut = useCallback(() => {
    discardCapabilities()
    setPlatformOperatorToken('')
    savedToken.current = ''
    setTokenState('')
    invalidateActuation()
  }, [discardCapabilities, invalidateActuation])

  const value = useMemo<PlatformAuthContextValue>(
    () => ({
      token,
      // The last role read for this sign-in. A routine re-read keeps it while it runs (the buttons must not blink
      // off once a minute) and also when it fails: the badge and the operator and admin controls of other pages go
      // on as before. Decisions do not rest on it then: `capsError` keeps them off until a read succeeds
      // (decisionBlockReason). A role that was reset and is not back yet leaves nothing at all.
      caps: capsQuery.data,
      capsLoading: capsQuery.isPending,
      capsError: capsQuery.isError,
      canOperate: capsQuery.data?.can_operate === true,
      canAdmin: capsQuery.data?.can_admin === true,
      setToken,
      signOut,
      refreshCapabilities,
    }),
    [token, capsQuery.data, capsQuery.isPending, capsQuery.isError, setToken, signOut, refreshCapabilities],
  )

  return <PlatformAuthContext.Provider value={value}>{children}</PlatformAuthContext.Provider>
}
