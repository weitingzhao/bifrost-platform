import { expect, it, vi } from 'vitest'
import { render, cleanup, screen } from '@testing-library/react'
import { RuntimeWorld, type ObservationSnapshot } from '@bifrost/ui/city/observation'
const engine = vi.hoisted(() => ({ create: vi.fn() }))
vi.mock('phaser', () => ({ default: { Scene: class {}, Game: engine.create } }))
it('draws all regions without creating a continuous game loop and cleans its clock on exit', () => {
  vi.useFakeTimers()
  const snapshot: ObservationSnapshot = {version:1,id:'fixture',provenance:'fixture',capturedAt:'2026-11-22T12:00:00Z',evidence:{},content:{tree:{id:'town',kind:'town',shape:'8x8',label:'Fixture',question:'State',dashboard:null,children:[]}}}
  render(<RuntimeWorld snapshot={snapshot}/>)
  for (const name of ['Domain village','CI workshop','Data warehouse','Infrastructure power yard']) expect(screen.getByRole('region',{name})).toBeTruthy()
  expect(engine.create).not.toHaveBeenCalled()
  cleanup()
  expect(vi.getTimerCount()).toBe(0)
  vi.useRealTimers()
})
