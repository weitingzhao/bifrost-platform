import { useState } from 'react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { platformAuthAuthenticatedBadgeClass, platformAuthRoleBadgeClass } from '@/lib/platformAuthUi'

export function PlatformAuthBar({
  compact = false,
  hideRefresh = false,
}: {
  compact?: boolean
  hideRefresh?: boolean
}) {
  const { token, caps, capsLoading, setToken, signOut, refreshCapabilities } = usePlatformAuth()
  const [authOpen, setAuthOpen] = useState(false)
  const [tokenInput, setTokenInput] = useState('')

  const role = (caps?.role ?? 'viewer').toLowerCase()
  const isAuthenticated = caps?.authenticated === true
  const isInvalidToken = token !== '' && !isAuthenticated && !capsLoading

  function handleConnect() {
    const next = tokenInput.trim()
    if (next === '') return
    setToken(next)
    setAuthOpen(false)
    setTokenInput('')
    queueMicrotask(() => refreshCapabilities())
  }

  function handleSignOut() {
    signOut()
    setAuthOpen(false)
    setTokenInput('')
  }

  return (
    <div className="platform-auth-bar">
      <div className="platform-auth-bar__row">
        <div className="platform-auth-bar__badges">
          <span className={`badge-ui ${platformAuthRoleBadgeClass(role)}`}>{role}</span>
          {!compact && caps?.principal != null && caps.principal !== '' && (
            <span className="text-[var(--muted-foreground)]">{caps.principal}</span>
          )}
          {isAuthenticated ? (
            <span className={`badge-ui ${platformAuthAuthenticatedBadgeClass()}`}>
              {compact ? 'Auth' : 'Authenticated'}
            </span>
          ) : isInvalidToken ? (
            <span className="badge-ui platform-auth-badge--warn">{compact ? 'Invalid' : 'Invalid token'}</span>
          ) : (
            <span className="badge-ui platform-auth-badge--warn">
              {compact ? 'No token' : 'Token required for control'}
            </span>
          )}
        </div>
        <div className="platform-auth-bar__actions">
          {!hideRefresh && (
            <button type="button" className="btn-ui text-xs" onClick={refreshCapabilities}>
              Refresh
            </button>
          )}
          {isAuthenticated ? (
            <button type="button" className="btn-ui text-xs" onClick={handleSignOut}>
              Sign out
            </button>
          ) : (
            <>
              <button
                type="button"
                className="btn-ui text-xs"
                onClick={() => setAuthOpen(open => !open)}
              >
                {token !== '' ? 'Change token' : 'Authenticate'}
              </button>
              {token !== '' ? (
                <button type="button" className="btn-ui text-xs" onClick={handleSignOut}>
                  Sign out
                </button>
              ) : null}
            </>
          )}
        </div>
      </div>
      {authOpen && !isAuthenticated && (
        <div className="platform-auth-bar__connect">
          <input
            type="password"
            value={tokenInput}
            onChange={event => setTokenInput(event.currentTarget.value)}
            onKeyDown={event => event.key === 'Enter' && handleConnect()}
            placeholder="Platform operator token…"
            className="platform-auth-bar__input"
            autoFocus
          />
          <button
            type="button"
            className="btn-ui btn-ui-primary text-xs"
            disabled={tokenInput.trim() === ''}
            onClick={handleConnect}
          >
            Connect
          </button>
        </div>
      )}
    </div>
  )
}
