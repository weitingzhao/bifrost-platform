import { expect, it } from 'vitest'
import { APPROVAL_WAITING_NOTICE } from '@/pages/approvalWaitingNotice'

it('explains that an approved action continues after closing the page', () => {
  expect(APPROVAL_WAITING_NOTICE).toBe(
    'Still waiting for the server. The action continues on the server even if this page is closed; reopen this page to see the result. This can take up to 5 minutes.',
  )
})
