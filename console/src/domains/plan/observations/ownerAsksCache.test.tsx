import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useOwnerAsks } from './ownerAsksModel'
afterEach(() => { cleanup(); vi.unstubAllGlobals() })
it('reads existing shell caches without issuing requests', () => {
 const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
 const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
 const questions = [{ id: 'cached-question' }], requests = [{ id: 'cached-request' }]
 client.setQueryData(['questions', 'all'], questions)
 client.setQueryData(['approvals', 'open'], requests)
 const { result } = renderHook(() => useOwnerAsks(), { wrapper: ({children}: {children: ReactNode}) => <QueryClientProvider client={client}>{children}</QueryClientProvider> })
 expect(result.current.questions).toMatchObject({state:'ok',data:questions})
 expect(result.current.requests).toMatchObject({state:'ok',data:requests})
 expect(fetch).not.toHaveBeenCalled()
 client.clear()
})
