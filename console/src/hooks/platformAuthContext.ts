import { createContext } from 'react'
import type { AuthCapabilities } from '@/api/matrixTypes'

export interface PlatformAuthContextValue {
  token: string
  /** Sign-in epoch. The token is not a cache key. */
  epoch: number
  caps: AuthCapabilities | undefined
  /** Epoch the capability answer belongs to, or null when this sign-in has none. */
  capsEpoch: number | null
  capsLoading: boolean
  canOperate: boolean
  canAdmin: boolean
  setToken: (token: string) => void
  signOut: () => void
  refreshCapabilities: () => void
}

export const PlatformAuthContext = createContext<PlatformAuthContextValue | null>(null)
