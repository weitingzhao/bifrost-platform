/**
 * One sign-in, counted in memory. The token is never part of a cache key:
 * a capability answer is stored under the epoch id, and a later sign-in
 * cannot read an earlier answer.
 */
export type SignInEpoch = {
  id: number
  token: string
}

let nextId = 0
let current: SignInEpoch = { id: 0, token: '' }

export function currentSignInEpoch(): SignInEpoch {
  return current
}

/** Sign in, sign out, replace, or a token change seen from another tab. */
export function beginSignInEpoch(token: string): SignInEpoch {
  nextId += 1
  current = { id: nextId, token }
  return current
}

/**
 * Strict Mode runs the state initializer twice and keeps the first result,
 * so the module can be one id ahead of the epoch React stored. Rewind to
 * that kept epoch. Later renders must not call this: a newer sign-in wins.
 */
export function restoreSignInEpoch(epoch: SignInEpoch): void {
  if (current.id === epoch.id + 1 && current.token === epoch.token) {
    current = epoch
  }
}

export function capabilityQueryKey(epochId: number): readonly ['platform', 'auth', 'capabilities', number] {
  return ['platform', 'auth', 'capabilities', epochId]
}
