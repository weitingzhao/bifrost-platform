import type { CityContent } from '../frame/content'
import { consoleFunctions, placeTree } from './consolePlaces'
import { panelsOf, pickedPanel } from './panels'

/** Today's content: the Console's menu as places, with greybox panels. A plug-in of the frame. */
export const consoleContent = (): CityContent => ({ tree: placeTree(), panels: panelsOf, picked: pickedPanel, functions: consoleFunctions() })
