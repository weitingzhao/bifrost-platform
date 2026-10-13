import { createContext } from 'react'
import type { AuthCapabilities } from '@/api/matrixTypes'

export interface PlatformAuthContextValue {
  token: string
  caps: AuthCapabilities | undefined
  capsLoading: boolean
  /** The capabilities read failed. Stale data must not be treated as a role. */
  capsError: boolean
  canOperate: boolean
  canAdmin: boolean
  setToken: (token: string) => void
  signOut: () => void
  refreshCapabilities: () => void
}

export const PlatformAuthContext = createContext<PlatformAuthContextValue | null>(null)
