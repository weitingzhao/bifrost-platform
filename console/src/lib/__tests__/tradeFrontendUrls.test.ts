import { describe, expect, it } from 'vitest'
import {
  resolveTradeFrontendUrls,
  tradeEnvChipClass,
  tradeFrontendEnvFromViewer,
} from '@/lib/tradeFrontendUrls'

describe('tradeFrontendUrls', () => {
  it('maps viewer seat to trade env', () => {
    expect(tradeFrontendEnvFromViewer('dev')).toBe('dev')
    expect(tradeFrontendEnvFromViewer('dev-local')).toBe('dev')
    expect(tradeFrontendEnvFromViewer('stg')).toBe('stg')
    expect(tradeFrontendEnvFromViewer('prod')).toBe('prod')
  })

  it('defaults to Traefik ingress HTTPS hosts', () => {
    const urls = resolveTradeFrontendUrls({} as ImportMetaEnv)
    expect(urls).toEqual({
      dev: 'https://dev.trader.bifrost.lan/',
      stg: 'https://stg.trader.bifrost.lan/',
      prod: 'https://trader.bifrost.lan/',
    })
  })

  it('prefers per-env overrides', () => {
    const urls = resolveTradeFrontendUrls({
      VITE_TRADE_FRONTEND_URL_DEV: 'http://dev.trade',
      VITE_TRADE_FRONTEND_URL_STG: 'http://stg.trade',
      VITE_TRADE_FRONTEND_URL_PROD: 'http://prod.trade',
    } as ImportMetaEnv)
    expect(urls).toEqual({
      dev: 'http://dev.trade',
      stg: 'http://stg.trade',
      prod: 'http://prod.trade',
    })
  })

  it('tradeEnvChipClass uses env tokens and strengthens when active', () => {
    expect(tradeEnvChipClass('dev', false)).toContain('--color-env-dev')
    expect(tradeEnvChipClass('stg', false)).toContain('--color-env-stg')
    expect(tradeEnvChipClass('prod', false)).toContain('--color-env-prod')
    expect(tradeEnvChipClass('dev', true)).toContain('font-semibold')
    expect(tradeEnvChipClass('prod', true)).not.toEqual(
      tradeEnvChipClass('prod', false),
    )
  })
})
