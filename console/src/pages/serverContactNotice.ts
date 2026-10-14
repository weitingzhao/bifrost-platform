export const NEVER_REACHED = 'this page has not reached the server yet'
export const NOT_SENT = 'Your decision was NOT sent. Reload this page and try again.'
export const NOT_CONFIRMED = 'The server has not confirmed it yet — do not assume it went through. Keep this page open, or reopen it later to see the result.'
export const NOT_HEARD = 'This page has not heard from the server since'
export const CHECKING_CONNECTION = 'Checking the connection to the server…'
export const DECISION_CHANGED = 'Your decision was NOT sent: your sign-in, your selection or the Console version changed while the connection was being checked. Review it and try again.'

/** Browser-local HH:MM:SS; the date is added when the moment is not today. */
export function localContactTime(time: number): string {
  const date = new Date(time)
  const pad = (n: number) => String(n).padStart(2, '0')
  const clock = [date.getHours(), date.getMinutes(), date.getSeconds()].map(pad).join(':')
  const today = new Date()
  const sameDay = date.getFullYear() === today.getFullYear() && date.getMonth() === today.getMonth() && date.getDate() === today.getDate()
  return sameDay ? clock : `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${clock}`
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
