import { useState } from 'react'
import { Button, Popover, PopoverContent, PopoverTrigger } from '@bifrost/ui'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { platformAuthAuthenticatedBadgeClass, platformAuthRoleBadgeClass } from '@/lib/platformAuthUi'

function TokenConnectPopover({
  triggerLabel,
  onConnect,
}: {
  triggerLabel: string
  onConnect: (token: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [tokenInput, setTokenInput] = useState('')

  function handleConnect() {
    const next = tokenInput.trim()
    if (next === '') return
    onConnect(next)
    setOpen(false)
    setTokenInput('')
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" size="xs">
          {triggerLabel}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" side="bottom" className="w-80 gap-2">
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Bearer token for operator or admin actuation routes.
        </p>
        <input
          type="password"
          value={tokenInput}
          onChange={event => setTokenInput(event.currentTarget.value)}
          onKeyDown={event => event.key === 'Enter' && handleConnect()}
          placeholder="Platform operator token…"
          className="platform-auth-bar__input w-full"
          autoFocus
        />
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="xs" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button size="xs" disabled={tokenInput.trim() === ''} onClick={handleConnect}>
            Connect
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export function PlatformAuthBar({
  compact = false,
  hideRefresh = false,
}: {
  compact?: boolean
  hideRefresh?: boolean
}) {
  const { token, caps, capsLoading, setToken, signOut, refreshCapabilities } = usePlatformAuth()

  const role = (caps?.role ?? 'viewer').toLowerCase()
  const isAuthenticated = caps?.authenticated === true
  const isInvalidToken = token !== '' && !isAuthenticated && !capsLoading

  function handleConnect(next: string) {
    setToken(next)
    queueMicrotask(() => refreshCapabilities())
  }

  function handleSignOut() {
    signOut()
    queueMicrotask(() => refreshCapabilities())
  }

  return (
    <div className="platform-auth-bar">
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
          <Button variant="outline" size="xs" onClick={refreshCapabilities}>
            Refresh
          </Button>
        )}
        {isAuthenticated ? (
          <>
            <TokenConnectPopover triggerLabel="Change token" onConnect={handleConnect} />
            <Button variant="outline" size="xs" onClick={handleSignOut}>
              Sign out
            </Button>
          </>
        ) : (
          <>
            <TokenConnectPopover
              triggerLabel={token !== '' ? 'Change token' : 'Authenticate'}
              onConnect={handleConnect}
            />
            {token !== '' ? (
              <Button variant="outline" size="xs" onClick={handleSignOut}>
                Sign out
              </Button>
            ) : null}
          </>
        )}
      </div>
    </div>
  )
}
