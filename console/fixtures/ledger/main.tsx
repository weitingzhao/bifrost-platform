import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useHardwareObservation } from '@/domains/foundation/observation-access'
import { HardwareLedger } from './HardwareLedger'
import './style.css'
const client = new QueryClient()
function Preview() {
  const query = useHardwareObservation()
  return <>{query.isError && <p role="alert">Hardware refresh failed: {String(query.error)}. Retained data uses its displayed capture time.</p>}{query.data ? <HardwareLedger latest={query.data} /> : <p role="status">Reading platform hardware…</p>}</>
}
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={client}><Preview /></QueryClientProvider>)
