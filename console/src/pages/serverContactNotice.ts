export const NEVER_REACHED = 'this page has not reached the server yet'
export const NOT_SENT = 'Your decision was NOT sent. Reload this page and try again.'
export const NOT_CONFIRMED = 'The server has not confirmed it yet — do not assume it went through. This page will show the result.'
export const NOT_HEARD = 'This page has not heard from the server since'
export const DECISION_CHANGED = 'Your decision was NOT sent because the record, sign-in, or Console version changed during the connection check. Review it and try again.'

export function localContactTime(time: number): string {
  const date = new Date(time)
  return [date.getHours(), date.getMinutes(), date.getSeconds()].map(n => String(n).padStart(2, '0')).join(':')
}
export function notSentNotice(last: number | null): string {
  return `The server did not answer a connection check (${last == null ? NEVER_REACHED : `last reached at ${localContactTime(last)}`}). ${NOT_SENT}`
}
export function sentNotice(time: number): string {
  return `Sent at ${localContactTime(time)}. ${NOT_CONFIRMED}`
}
export function notHeardNotice(time: number): string {
  return `${NOT_HEARD} ${localContactTime(time)}.`
}
