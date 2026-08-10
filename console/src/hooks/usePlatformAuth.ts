import { useContext } from 'react'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'

export function usePlatformAuth(): PlatformAuthContextValue {
  const ctx = useContext(PlatformAuthContext)
  if (ctx == null) {
    throw new Error('usePlatformAuth must be used within PlatformAuthProvider')
  }
  return ctx
}
