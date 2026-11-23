import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RuntimeWorld, type ObservationSnapshot } from '@bifrost/ui/city/observation'
const engine = vi.hoisted(() => ({ wake: vi.fn(), sleep: vi.fn(), destroy: vi.fn(), create: vi.fn() }))
vi.mock('phaser', () => ({default: {Scene: class {}, Game: class { loop = {wake: engine.wake, sleep: engine.sleep}; constructor(config: unknown) { engine.create(config) } destroy = engine.destroy }, CANVAS: 1, Scale: {RESIZE: 0}}}))
afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.clearAllMocks()})
const snapshot: ObservationSnapshot = {version:1,id:'fixture',capturedAt:'2026-11-22T12:00:00Z',provenance:'fixture',evidence:{},content:{tree:{id:'root',label:'Root',kind:'town',shape:'4x4',question:'',dashboard:null,children:[]}}}
// Owner 2026-11-22: prevent continuous rendering, hidden-page work and leaked engines.
it('sleeps when idle/hidden, wakes on input, and destroys once on exit', async () => {
  const {unmount} = render(<RuntimeWorld snapshot={snapshot}/>)
  await waitFor(() => expect(engine.create).toHaveBeenCalledTimes(1))
  expect(engine.create.mock.calls[0][0]).toMatchObject({type:1,fps:{target:20}})
  await waitFor(() => expect(engine.sleep).toHaveBeenCalled())
  const wakeCount = engine.wake.mock.calls.length
  fireEvent.pointerDown(screen.getByRole('img'))
  expect(engine.wake.mock.calls.length).toBeGreaterThan(wakeCount)
  const hidden = vi.spyOn(document,'hidden','get').mockReturnValue(true)
  fireEvent(document, new Event('visibilitychange'))
  const asleep = engine.wake.mock.calls.length
  fireEvent.pointerMove(screen.getByRole('img'))
  expect(engine.wake).toHaveBeenCalledTimes(asleep)
  hidden.mockReturnValue(false)
  fireEvent(document, new Event('visibilitychange'))
  expect(engine.wake.mock.calls.length).toBeGreaterThan(asleep)
  unmount()
  expect(engine.destroy).toHaveBeenCalledExactlyOnceWith(true)
  const stopped = engine.wake.mock.calls.length
  fireEvent(document, new Event('visibilitychange'))
  expect(engine.wake).toHaveBeenCalledTimes(stopped)
})
