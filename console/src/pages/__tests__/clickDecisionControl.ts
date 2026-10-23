import { fireEvent } from '@testing-library/react'
import { flushSync } from 'react-dom'

/** Exercise the real review step before approving; other decision controls remain one click. */
export function clickDecisionControl(element: HTMLElement): void {
  if (element.textContent?.trim() === 'Review approval') {
    flushSync(() => { fireEvent.click(element) })
  }
  fireEvent.click(element)
}
