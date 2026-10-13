import { createContext } from 'react'
import type { AuthCapabilities } from '@/api/matrixTypes'

export interface PlatformAuthContextValue {
  token: string
  /** The last role read for this sign-in. It stays through a routine re-read, including one that failed. */
  caps: AuthCapabilities | undefined
  capsLoading: boolean
  /**
   * The latest capabilities read failed. `caps`, `canOperate` and `canAdmin` may still hold the role read before
   * it; a decision must not rest on that (decisionBlockReason checks this before it looks at the role).
   */
  capsError: boolean
  canOperate: boolean
  canAdmin: boolean
  setToken: (token: string) => void
  signOut: () => void
  refreshCapabilities: () => void
}

export const PlatformAuthContext = createContext<PlatformAuthContextValue | null>(null)
