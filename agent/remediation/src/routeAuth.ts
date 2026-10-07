import { timingSafeEqual } from 'node:crypto'
import type { NextFunction, Request, Response } from 'express'

/** Shared bearer for runner and Hermes non-health routes. Value lives only in the mini .env. */
export const RUNNER_TOKEN_ENV = 'REMEDIATION_RUNNER_TOKEN'

/** POST /run/:id/respond accepts this token and rejects the runner token. */
export const OPERATOR_TOKEN_ENV = 'PLATFORM_OPERATOR_TOKEN'

export function isLoopback(host: string): boolean {
  const value = host.trim().toLowerCase()
  return value === '127.0.0.1' || value === 'localhost' || value === '::1' || value === '[::1]'
}

/** Non-empty when the process must not listen. The message names env keys, never values. */
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

function presentedBearer(header: string | undefined): string | null {
  if (header == null) return null
  const match = /^Bearer\s+(\S+)\s*$/i.exec(header)
  return match?.[1] ?? null
}

export type RunnerAuth = {
  runnerToken: string
  operatorToken: string
}

function isRespond(req: Request): boolean {
  return req.method === 'POST' && /^\/run\/[^/]+\/respond$/.test(req.path)
}

/**
 * Every route except GET/HEAD /health requires a bearer.
 * /run/:id/respond requires the operator token and rejects the runner token,
 * so a job that holds the runner secret cannot approve itself.
 */
export function requireBearer(auth: RunnerAuth) {
  const runnerToken = auth.runnerToken.trim()
  const operatorToken = auth.operatorToken.trim()
  return function requireBearer(req: Request, res: Response, next: NextFunction): void {
    if ((req.method === 'GET' || req.method === 'HEAD') && req.path === '/health') {
      next()
      return
    }
    if (runnerToken === '' && operatorToken === '') {
      next()
      return
    }
    const presented = presentedBearer(req.header('authorization'))
    if (presented == null) {
      res.status(401).json({ error: 'bearer token required' })
      return
    }
    if (isRespond(req)) {
      if (operatorToken === '' || !tokenEquals(presented, operatorToken)) {
        res.status(401).json({ error: 'operator token required' })
        return
      }
      if (runnerToken !== '' && tokenEquals(presented, runnerToken)) {
        res.status(401).json({ error: 'operator token required' })
        return
      }
      next()
      return
    }
    if (runnerToken === '' || !tokenEquals(presented, runnerToken)) {
      res.status(401).json({ error: 'bearer token required' })
      return
    }
    next()
  }
}
