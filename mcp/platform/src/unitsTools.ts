/**
 * The paths of the units routes the CI tools read (index.ts registers the
 * tools). Kept apart so the query strings are tested without a server.
 */
const q = (pairs: Array<[string, string | number | undefined]>): string => {
  const kept = pairs.filter(([, v]) => v !== undefined && v !== '')
  return kept.length === 0 ? '' : '?' + kept.map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`).join('&')
}

export const unitScopePath = (repo: string, commit: string, base?: string): string =>
  '/api/v1/units/scope' + q([['repo', repo], ['commit', commit], ['base', base]])

export const unitConfigPath = (repo: string, commit?: string): string =>
  '/api/v1/units/config' + q([['repo', repo], ['commit', commit]])

export const unitRunsPath = (repo: string, limit?: number): string =>
  '/api/v1/units/runs' + q([['repo', repo], ['limit', limit]])

export const FULL_CI_PATH = '/api/v1/units/full-ci'

export const fullCICheckPath = (repo: string, green: string, red: string, run?: string): string =>
  '/api/v1/units/full-ci/check' + q([['repo', repo], ['green', green], ['red', red], ['run', run]])
