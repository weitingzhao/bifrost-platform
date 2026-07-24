/** Smooth-scroll the viewport to an element by id (Verdict → Body continuity). */
export function scrollToSection(id: string): void {
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
