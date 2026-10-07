import { timingSafeEqual } from 'node:crypto'
import type { NextFunction, Request, Response } from 'express'

/** Same key the remediation runner uses. Value lives only in the mini .env. */
export const RUNNER_TOKEN_ENV = 'REMEDIATION_RUNNER_TOKEN'

export function isLoopback(host: string): boolean {
  const value = host.trim().toLowerCase()
  return value === '127.0.0.1' || value === 'localhost' || value === '::1' || value === '[::1]'
}

/** Non-empty when the process must not listen. The message names the env key, never a value. */
export function bindRefusal(bindHost: string, runnerToken: string): string | null {
  if (!isLoopback(bindHost) && runnerToken.trim() === '') {
    return `refusing to start: bind ${bindHost} is not loopback and ${RUNNER_TOKEN_ENV} is unset`
  }
  return null
}

function tokenEquals(presented: string, expected: string): boolean {
  const a = Buffer.from(presented)
  const b = Buffer.from(expected)
  if (a.length !== b.length) {
    timingSafeEqual(b, b)
    return false
  }
  return timingSafeEqual(a, b)
}

/**
 * Non-GET routes require the shared runner bearer. GET stays open so health
 * probes and the platform-api skill catalog keep working without this secret.
 * When the token is unset the process may only bind loopback, and routes stay open.
 */
export function requireBearer(runnerToken: string) {
  const expected = runnerToken.trim()
  return function requireBearer(req: Request, res: Response, next: NextFunction): void {
    if (req.method === 'GET' || req.method === 'HEAD' || expected === '') {
      next()
      return
    }
    const match = /^Bearer\s+(\S+)\s*$/i.exec(req.header('authorization') ?? '')
    const presented = match?.[1] ?? ''
    const ok =
      presented !== '' &&
      presented.length === expected.length &&
      tokenEquals(presented, expected)
    if (!ok) {
      res.status(401).json({ error: 'bearer token required' })
      return
    }
    next()
  }
}
