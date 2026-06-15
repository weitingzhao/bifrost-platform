import { ShellNavSidebar } from '@bifrost/ui'
import { CONSOLE_NAV_GROUPS } from '@/lib/consoleNavConfig'

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

export type ConsoleViewTab =
  | 'briefing'
  | 'control-room'
  | 'pulse'
  | 'runtime-map'
  | 'cluster'
  | 'delivery'
  | 'program'
  | 'promote'
  | 'blueprint'
  | 'environments'
  | 'platform-standards'
  | 'agent-protocol'
  | 'design-system'
  | 'console'

type DocLink = { id: string; label: string; href: string }

export function ConsoleSidebar({
  activeTab,
  onSelect,
  docLinks,
}: {
  activeTab: string
  onSelect: (id: string) => void
  docLinks: DocLink[]
}) {
  return (
    <ShellNavSidebar
      productName="Bifrost Ops"
      productBadge="Ops"
      navGroups={CONSOLE_NAV_GROUPS}
      activeId={activeTab}
      onSelect={(item) => onSelect(item.id)}
      peerApp={{
        label: 'Bifrost Trade Monitoring',
        href: TRADE_APP_URL,
        description: 'Business console · positions, daemon, market',
      }}
      docLinks={docLinks}
      storageKey="bifrost-ops"
    />
  )
}
