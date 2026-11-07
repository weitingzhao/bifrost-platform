const pad = (n: number) => String(n).padStart(2, '0')
/** HH:MM on this browser's clock, for a stage strip; the full time stays on the element. */
export function clockText(iso: string): string {
  const d = new Date(iso)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}
