import type { ReactNode } from 'react'

/**
 * The little picture beside each row of a facility's key: the part as it looks on the facility, in its
 * colours (lit windows #ffd76a, water #5aa9f0, fuel #c98a3a), so the eye matches the key to the drawing.
 */
const INK = '#2f2d3a', LIT = '#ffd76a', GLASS = '#8fa3b8', WATER = '#5aa9f0', FUEL = '#c98a3a'

const tank = (x: number, r: number, f: number, c: string) => <g>
  <rect x={x - r} y={3} width={2 * r} height={14} rx={r * 0.6} fill="#e8e6e0" stroke={INK} strokeWidth={1} />
  <rect x={x - r + 1.5} y={3 + 14 * (1 - f) + 1} width={2 * r - 3} height={14 * f - 2} rx={1} fill={c} />
</g>

const ICONS: Record<string, ReactNode> = {
  'cpu.busy': <g>{[0, 1, 2, 3, 4].map(k => <rect key={k} x={2 + k * 5.2} y={4} width={3.6} height={12} fill={k < 3 ? LIT : GLASS} stroke={INK} strokeWidth={0.8} />)}</g>,
  'cpu.smoke': <g><rect x={6} y={10} width={5} height={10} fill="#9aa3b0" stroke={INK} strokeWidth={0.8} /><circle cx={10} cy={7} r={3} fill="#fff" stroke="#b8b8c0" /><circle cx={15} cy={4} r={3.6} fill="#fff" stroke="#b8b8c0" /><circle cx={21} cy={3} r={2.6} fill="#fff" stroke="#b8b8c0" opacity={0.5} /></g>,
  cores: <g><rect x={3} y={11} width={8} height={8} fill="#f0a35a" stroke={INK} strokeWidth={0.8} /><rect x={15} y={3} width={8} height={16} fill="#f0a35a" stroke={INK} strokeWidth={0.8} /></g>,
  'mem.used': tank(13, 6, 0.55, WATER),
  'mem.total': <g>{tank(7, 4, 0.5, WATER)}{tank(19, 6, 0.5, WATER)}</g>,
  'disk.used': tank(13, 6, 0.55, FUEL),
  'disk.total': <g>{tank(7, 4, 0.5, FUEL)}{tank(19, 6, 0.5, FUEL)}</g>,
  'volume.used': <g><rect x={4} y={4} width={18} height={15} fill="#3a3d48" stroke={INK} strokeWidth={0.8} />{[0, 1].map(q => <rect key={q} x={6} y={14 - q * 5} width={14} height={4} fill={FUEL} stroke={INK} strokeWidth={0.5} />)}</g>,
  'volume.disks': <g>{[0, 1, 2].map(k => <g key={k}><rect x={2 + k * 8} y={7} width={6} height={12} fill="#2c2e38" stroke={INK} strokeWidth={0.6} /><circle cx={5 + k * 8} cy={4} r={1.8} fill={k < 2 ? '#5ce07a' : '#ff4d4d'} /></g>)}</g>,
  pods: <g><rect x={3} y={4} width={20} height={13} rx={1.5} fill="#1d2a3a" stroke={INK} strokeWidth={0.8} /><text x={13} y={14} textAnchor="middle" fontSize={8} fontWeight={700} fill="#7be08f">38</text></g>,
  identity: <g><path d="M13 2 L24 8 L13 14 L2 8 Z" fill="#d8d4c8" stroke={INK} strokeWidth={0.8} /><text x={13} y={11} textAnchor="middle" fontSize={7} fontWeight={800} fill={INK}>01</text></g>,
  purpose: <g><rect x={1} y={6} width={24} height={9} fill="#fbf6e4" stroke={INK} strokeWidth={0.8} /><text x={13} y={13} textAnchor="middle" fontSize={6} fontWeight={700} fill={INK}>POOL</text></g>,
  state: <g><circle cx={6} cy={10} r={3.2} fill="#5ce07a" stroke={INK} strokeWidth={0.7} /><circle cx={13} cy={10} r={3.2} fill="#a4a8b2" stroke={INK} strokeWidth={0.7} /><circle cx={20} cy={10} r={3.2} fill="#ff4d4d" stroke={INK} strokeWidth={0.7} /></g>,
}

export function KeyIcon({ id }: { id: string }) {
  return <svg className="g2-key-icon" viewBox="0 0 26 20" width={26} height={20} aria-hidden="true">{ICONS[id] ?? null}</svg>
}
