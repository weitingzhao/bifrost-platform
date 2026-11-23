import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { LiveRefresh } from '@/domains/foundation/bus/liveRefresh'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { registerServiceWorker } from '@/pwa/serviceWorker'
import { ConsoleRootBoundary } from '@/pwa/ConsoleCrashScreen'
import './index.css'
import './styles/opsAppearance.css'
import './styles/opsShell.css'
import './styles/opsForYou.css'
import './styles/opsWorkspace.css'
import './styles/opsDecisions.css'

registerServiceWorker()

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 10_000,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConsoleRootBoundary>
      <QueryClientProvider client={queryClient}>
        <LiveRefresh queryClient={queryClient} />
        <PlatformAuthProvider>
          <App />
        </PlatformAuthProvider>
      </QueryClientProvider>
    </ConsoleRootBoundary>
  </StrictMode>
)
