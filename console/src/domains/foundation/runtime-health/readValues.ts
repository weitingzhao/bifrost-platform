export type RuntimeRecord = Record<string, unknown>
export const record = (v: unknown): RuntimeRecord => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as RuntimeRecord : {}
export const records = (v: unknown): RuntimeRecord[] => Array.isArray(v) ? v.filter(x => x !== null && typeof x === 'object' && !Array.isArray(x)) as RuntimeRecord[] : []
export const text = (v: unknown): string => typeof v === 'string' ? v : ''
export const number = (v: unknown): number | undefined => typeof v === 'number' && Number.isFinite(v) ? v : undefined
export const strings = (v: unknown): string[] => Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
