import type { Sprite } from '../kit'
import { archive, launchPad, missionControl, supplierDock } from './landmarks'

/**
 * The first generation's landmarks a place can stand as (the Owner, 2026-11-20: its buildings were the
 * beautiful part; bring them over, one class at a time). The frame draws the landmark scaled into the
 * place's footprint; which place stands as which is content's call, and says what the landmark means there.
 * A landmark drawn here shows nothing that is not read: the launch pad stands empty until a reading puts
 * a vehicle on it.
 */
export type LandmarkId = 'launchPad' | 'archive' | 'missionControl' | 'supplierDock'

/** Each landmark's footprint in cells (n×n) and how tall it stands (px at that footprint), and how it is drawn. */
export const LANDMARKS: Record<LandmarkId, { n: number; height: number; make: () => Sprite }> = {
  launchPad: { n: 4, height: 250, make: () => launchPad(true) },
  archive: { n: 2, height: 88, make: archive },
  missionControl: { n: 4, height: 110, make: missionControl },
  supplierDock: { n: 2, height: 60, make: () => supplierDock() },
}
