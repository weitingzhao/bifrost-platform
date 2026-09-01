/** Landing tab for Research Engine when a retired hash (#analytics-pipeline) redirects here. */

export type ResearchEngineTab = 'health' | 'catalog' | 'accuracy' | 'cost' | 'runs'

const LANDING_KEY = 'research-engine-landing-tab'

const TABS: readonly ResearchEngineTab[] = ['health', 'catalog', 'accuracy', 'cost', 'runs']

function isTab(value: string | null): value is ResearchEngineTab {
  return value != null && (TABS as readonly string[]).includes(value)
}

export function setResearchEngineLandingTab(tab: ResearchEngineTab): void {
  sessionStorage.setItem(LANDING_KEY, tab)
}

export function consumeResearchEngineLandingTab(): ResearchEngineTab {
  const raw = sessionStorage.getItem(LANDING_KEY)
  sessionStorage.removeItem(LANDING_KEY)
  return isTab(raw) ? raw : 'health'
}
