import type { Reachability } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'

const GAUGE_CX = 50
const GAUGE_CY = 52
const GAUGE_R = 36
const NEEDLE_MIN = -90
const NEEDLE_MAX = 90

function reachColor(reach: Reachability | undefined): string {
  switch (reach) {
    case 'fail':
      return 'var(--color-lamp-red)'
    case 'degraded':
      return 'var(--color-lamp-yellow)'
    case 'ok':
      return 'var(--primary)'
    default:
      return 'var(--color-lamp-gray)'
  }
}

function polarXY(cx: number, cy: number, r: number, deg: number) {
  const rad = ((deg - 90) * Math.PI) / 180
  return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) }
}

function arcPath(cx: number, cy: number, r: number, startDeg: number, endDeg: number) {
  const start = polarXY(cx, cy, r, endDeg)
  const end = polarXY(cx, cy, r, startDeg)
  const large = endDeg - startDeg <= 180 ? 0 : 1
  return `M ${start.x} ${start.y} A ${r} ${r} 0 ${large} 0 ${end.x} ${end.y}`
}

function formatAllocLabel(raw: string | undefined): string | undefined {
  if (raw == null || raw === '') return undefined
  const ki = /^(\d+)Ki$/.exec(raw)
  if (ki != null) {
    const gi = Number(ki[1]) / (1024 * 1024)
    return `${gi >= 10 ? gi.toFixed(0) : gi.toFixed(1)}Gi alloc`
  }
  return `${raw} alloc`
}

interface ClusterAnalogGaugeProps {
  label: string
  value: number | undefined
  max?: number
  display?: string
  sublabel?: string
  /** plain = show sublabel as-is; resource = format CPU/memory alloc suffix */
  sublabelMode?: 'plain' | 'resource'
  reach?: Reachability
  unavailable?: boolean
}

export function ClusterRadialGauge({
  label,
  value,
  max = 100,
  display,
  sublabel,
  sublabelMode = 'resource',
  reach = 'ok',
  unavailable = false,
}: ClusterAnalogGaugeProps) {
  const pct =
    unavailable || value == null
      ? 0
      : Math.min(100, Math.max(0, (value / max) * 100))
  const centerText =
    display ??
    (unavailable || value == null ? '—' : max === 100 ? `${pct.toFixed(1)}%` : String(value))
  const accent = unavailable ? 'var(--color-lamp-gray)' : reachColor(reach)
  const needleDeg = NEEDLE_MIN + (pct / 100) * (NEEDLE_MAX - NEEDLE_MIN)
  const arcStart = -120
  const arcEnd = 120
  const valueEnd = arcStart + (pct / 100) * (arcEnd - arcStart)
  const trackPath = arcPath(GAUGE_CX, GAUGE_CY, GAUGE_R, arcStart, arcEnd)
  const valuePath =
    pct > 0 ? arcPath(GAUGE_CX, GAUGE_CY, GAUGE_R, arcStart, valueEnd) : ''
  const allocLabel =
    sublabelMode === 'plain'
      ? sublabel
      : formatAllocLabel(sublabel?.replace(/ alloc$/, '')) ?? sublabel

  return (
    <div className="cluster-kpi-gauge cluster-kpi-gauge--analog">
      <svg
        className="cluster-kpi-gauge__svg"
        viewBox="0 0 100 68"
        role="img"
        aria-label={`${label} ${centerText}`}
      >
        <path
          className="cluster-kpi-gauge__track"
          d={trackPath}
          fill="none"
          strokeWidth="5"
          strokeLinecap="round"
        />
        {valuePath !== '' && (
          <path
            d={valuePath}
            fill="none"
            stroke={accent}
            strokeWidth="5"
            strokeLinecap="round"
          />
        )}
        {[-120, -60, 0, 60, 120].map(tick => {
          const outer = polarXY(GAUGE_CX, GAUGE_CY, GAUGE_R + 2, tick)
          const inner = polarXY(GAUGE_CX, GAUGE_CY, GAUGE_R - 5, tick)
          return (
            <line
              key={tick}
              x1={inner.x}
              y1={inner.y}
              x2={outer.x}
              y2={outer.y}
              className="cluster-kpi-gauge__tick"
              strokeWidth="1"
            />
          )
        })}
        <g transform={`translate(${GAUGE_CX} ${GAUGE_CY})`}>
          <line
            x1="0"
            y1="4"
            x2="0"
            y2={-GAUGE_R + 10}
            stroke={accent}
            strokeWidth="2"
            strokeLinecap="round"
            transform={`rotate(${needleDeg})`}
            className="cluster-kpi-gauge__needle"
          />
          <circle r="3.5" className="cluster-kpi-gauge__hub" fill={accent} />
          <circle r="1.5" className="cluster-kpi-gauge__hub-cap" />
        </g>
        <text
          x={GAUGE_CX}
          y={GAUGE_CY + 14}
          textAnchor="middle"
          className="cluster-kpi-gauge__readout font-mono-tabular"
        >
          {centerText}
        </text>
      </svg>
      <div className="cluster-kpi-gauge__meta">
        <span className="cluster-kpi-gauge__label">{label}</span>
        {allocLabel != null && allocLabel !== '' && (
          <span className="cluster-kpi-gauge__sublabel font-mono-tabular">{allocLabel}</span>
        )}
      </div>
    </div>
  )
}

interface ClusterStatTileProps {
  label: string
  value: string
  reach: Reachability
  hint?: string
}

export function ClusterStatTile({ label, value, reach, hint }: ClusterStatTileProps) {
  return (
    <div className="cluster-kpi-tile">
      <div className="cluster-kpi-tile__head">
        <StatusLamp value={reach} kind="reach" />
        <span className="cluster-kpi-tile__label">{label}</span>
      </div>
      <span className="cluster-kpi-tile__value font-mono-tabular">{value}</span>
      {hint != null && hint !== '' && (
        <span className="cluster-kpi-tile__hint font-mono-tabular">{hint}</span>
      )}
    </div>
  )
}
