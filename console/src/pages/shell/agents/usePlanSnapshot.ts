import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchPlanSnapshot } from '@/api/planSnapshot'
import { canonicalQuery } from '@/lib/shell/useHashSection'

function address() {
  const query = new URLSearchParams(canonicalQuery(window.location.hash))
  return { root: query.get('root') || query.get('round') || 'R4', node: query.get('node') || '' }
}
export function usePlanAddress() {
  const [value, setValue] = useState(address)
  useEffect(() => {
    const update = () => setValue(address())
    window.addEventListener('hashchange', update)
    return () => window.removeEventListener('hashchange', update)
  }, [])
  return value
}
export function usePlanSnapshot(root: string, enabled = true) {
  return useQuery({ enabled, queryKey: ['agent-runtime-plan-view', root], queryFn: ({ signal }) => fetchPlanSnapshot(root, signal), refetchInterval: 15_000, retry: false })
}
export function planNodeHref(root: string, node = '') {
  return `#work?${new URLSearchParams({ section: 'plan', root, ...(node ? { node } : {}) })}`
}
export const planNodeDOMId = (id: string) => `plan-node-${id}`
