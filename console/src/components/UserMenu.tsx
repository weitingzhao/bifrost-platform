import { useState } from 'react'
import { Button, StatusLamp, cn } from '@bifrost/ui'
import { BookOpen, RefreshCw, User } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { GUIDES_DEFAULT_TAB } from '@/lib/consoleNavConfig'
import {
  platformAuthAuthenticatedBadgeClass,
  platformAuthRoleBadgeClass,
} from '@/lib/platformAuthUi'

export type UserMenuProps = {
  /** Navigate to a console tab (Guides landing, etc.). */
  onSelectTab: (tabId: string) => void
  /** Ops platform-api health. */
  opsApiHealthy: boolean | undefined
  /** Refresh matrices / context / health. */
  onRefresh: () => void
  className?: string
}

/**
 * Shell User menu — Session · Guides (single entry) · Shell.
 * Governance doc detail lives in Guides settings nav, not this dropdown.
 */
export function UserMenu({
  onSelectTab,
  opsApiHealthy,
  onRefresh,
  className,
}: UserMenuProps) {
  const [open, setOpen] = useState(false)
  const [showTokenForm, setShowTokenForm] = useState(false)
  const [tokenInput, setTokenInput] = useState('')
  const { token, caps, capsLoading, setToken, signOut, refreshCapabilities } =
    usePlatformAuth()

  const role = (caps?.role ?? 'viewer').toLowerCase()
  const isAuthenticated = caps?.authenticated === true
  const isInvalidToken = token !== '' && !isAuthenticated && !capsLoading

  function handleConnect() {
    const next = tokenInput.trim()
    if (next === '') return
    setToken(next)
    setTokenInput('')
    setShowTokenForm(false)
    queueMicrotask(() => refreshCapabilities())
  }

  function handleSignOut() {
    signOut()
    setShowTokenForm(false)
    queueMicrotask(() => refreshCapabilities())
  }

  function handleSelectTab(tabId: string) {
    onSelectTab(tabId)
    setOpen(false)
  }

  return (
    <DropdownMenu
      open={open}
      onOpenChange={next => {
        setOpen(next)
        if (!next) {
          setShowTokenForm(false)
          setTokenInput('')
        }
      }}
    >
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className={cn(
            'h-7 shrink-0 gap-1.5 px-2 shadow-sm',
            isAuthenticated
              ? 'border-[color-mix(in_srgb,var(--color-env-dev)_45%,var(--border))]'
              : 'border-border',
            className,
          )}
          title="User menu — session, guides, shell"
          aria-label="User menu"
        >
          <User size={14} aria-hidden />
          <span className="text-[var(--text-dense-caption)] font-semibold">User</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-56" align="end" sideOffset={6}>
        <DropdownMenuLabel>Session</DropdownMenuLabel>
        <DropdownMenuGroup>
          <div className="flex flex-wrap items-center gap-1.5 px-2 py-1.5">
            <span className={`badge-ui ${platformAuthRoleBadgeClass(role)}`}>{role}</span>
            {isAuthenticated ? (
              <span className={`badge-ui ${platformAuthAuthenticatedBadgeClass()}`}>
                Authenticated
              </span>
            ) : isInvalidToken ? (
              <span className="badge-ui platform-auth-badge--warn">Invalid token</span>
            ) : (
              <span className="badge-ui platform-auth-badge--warn">Token required</span>
            )}
          </div>
          {showTokenForm ? (
            <div
              className="flex flex-col gap-1.5 px-2 pb-1.5"
              onKeyDown={event => event.stopPropagation()}
            >
              <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
                Bearer token for operator or admin actuation routes.
              </p>
              <input
                type="password"
                value={tokenInput}
                onChange={event => setTokenInput(event.currentTarget.value)}
                onKeyDown={event => {
                  if (event.key === 'Enter') handleConnect()
                }}
                placeholder="Platform operator token…"
                className="platform-auth-bar__input w-full"
                autoFocus
              />
              <div className="flex justify-end gap-1.5">
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => {
                    setShowTokenForm(false)
                    setTokenInput('')
                  }}
                >
                  Cancel
                </Button>
                <Button
                  size="xs"
                  disabled={tokenInput.trim() === ''}
                  onClick={handleConnect}
                >
                  Connect
                </Button>
              </div>
            </div>
          ) : (
            <div className="flex flex-wrap items-center gap-1.5 px-2 pb-1.5">
              <Button
                variant="outline"
                size="xs"
                onClick={() => setShowTokenForm(true)}
              >
                {isAuthenticated || token !== '' ? 'Change token' : 'Authenticate'}
              </Button>
              {isAuthenticated || token !== '' ? (
                <Button variant="outline" size="xs" onClick={handleSignOut}>
                  Sign out
                </Button>
              ) : null}
            </div>
          )}
        </DropdownMenuGroup>

        <DropdownMenuSeparator />

        <DropdownMenuItem onSelect={() => handleSelectTab(GUIDES_DEFAULT_TAB)}>
          <BookOpen aria-hidden />
          Guides
          <span className="ml-auto text-[var(--text-dense-meta)] text-muted-foreground">
            Docs
          </span>
        </DropdownMenuItem>

        <DropdownMenuSeparator />

        <DropdownMenuLabel>Shell</DropdownMenuLabel>
        <DropdownMenuGroup>
          <div className="flex items-center justify-between gap-2 px-2 py-1.5">
            <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
              Ops API
              <StatusLamp
                value={
                  opsApiHealthy === true
                    ? 'ok'
                    : opsApiHealthy === false
                      ? 'fail'
                      : 'unknown'
                }
                kind="reach"
              />
            </span>
            <Button
              type="button"
              variant="outline"
              size="xs"
              className="gap-1"
              onClick={() => {
                onRefresh()
                setOpen(false)
              }}
            >
              <RefreshCw size={11} aria-hidden />
              Refresh
            </Button>
          </div>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
