import { createContext } from 'react'
import type { AuthCapabilities } from '@/api/matrixTypes'

export interface PlatformAuthContextValue {
  token: string
  caps: AuthCapabilities | undefined
  capsLoading: boolean
  canOperate: boolean
  canAdmin: boolean
  setToken: (token: string) => void
  signOut: () => void
  refreshCapabilities: () => void
}

export const PlatformAuthContext = createContext<PlatformAuthContextValue | null>(null)
