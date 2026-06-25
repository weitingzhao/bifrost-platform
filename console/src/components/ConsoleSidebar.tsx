import { ShellNavSidebar } from '@bifrost/ui'
import { CONSOLE_NAV_GROUPS } from '@/lib/consoleNavConfig'

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

export type ConsoleViewTab =
  | 'agent-desk'
  | 'briefing'
  | 'control-room'
  | 'audit'
  | 'runtime-map'
  | 'cluster'
  | 'placement'
  | 'delivery'
  | 'program'
  | 'promote'
  | 'deploy-mainline'
  | 'platform-release'
  | 'blueprint'
  | 'environments'
  | 'roadmap'
  | 'k3s-architecture'
  | 'k3s-bootstrap'
  | 'cicd-bootstrap'
  | 'data-layer'
  | 'platform-standards'
  | 'agent-protocol'
  | 'mcp-contract'
  | 'design-system'
  | 'flywheel-vision'
  | 'console'

export function ConsoleSidebar({
  activeTab,
  onSelect,
}: {
  activeTab: string
  onSelect: (id: string) => void
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
      storageKey="bifrost-ops"
    />
  )
}
