import { createElement, type ReactNode } from 'react'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'

export type DecisionKind = 'admin' | 'operator' | 'viewer' | 'guest' | 'unknown'

export function sessionTokenFor(kind: DecisionKind): string {
  if (kind === 'guest') return ''
  if (kind === 'operator') return 'operator-token'
  if (kind === 'viewer') return 'viewer-token'
  if (kind === 'unknown') return 'session-token'
  return 'admin-token'
}

export function applySession(kind: DecisionKind): void {
  const token = sessionTokenFor(kind)
  if (token === '') window.localStorage.removeItem(PLATFORM_TOKEN_KEY)
  else window.localStorage.setItem(PLATFORM_TOKEN_KEY, token)
}

export function authValue(kind: DecisionKind): PlatformAuthContextValue {
  const token = sessionTokenFor(kind)
  const actions = {
    setToken: () => undefined,
    signOut: () => undefined,
    refreshCapabilities: () => undefined,
  }
  if (kind === 'unknown') {
    return { ...actions, token, caps: undefined, capsLoading: true, canOperate: false, canAdmin: false }
  }
  const caps: AuthCapabilities =
    kind === 'guest'
      ? { authenticated: false, role: 'viewer', can_operate: false, can_admin: false }
      : {
          authenticated: true,
          role: kind,
          can_operate: kind !== 'viewer',
          can_admin: kind === 'admin',
        }
  return {
    ...actions,
    token,
    caps,
    capsLoading: false,
    canOperate: caps.can_operate,
    canAdmin: caps.can_admin,
  }
}

export function withDecisionAuth(kind: DecisionKind, children: ReactNode) {
  return createElement(PlatformAuthContext.Provider, { value: authValue(kind) }, children)
}
