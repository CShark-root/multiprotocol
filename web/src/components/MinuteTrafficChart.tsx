import { useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'
import type { MinuteTraffic } from '@/lib/api'
import { formatBytes } from '@/lib/utils'

const LEFT = 58
const RIGHT = 12
const BOTTOM = 24
const TOP = 12

export function MinuteTrafficChart({ data, height = 190 }: { data: MinuteTraffic[]; height?: number }) {
  const { t, i18n } = useTranslation()
  const lng = i18n.language
  const wrap = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [active, setActive] = useState<number | null>(null)
  const [table, setTable] = useState(false)
  const [rangeMinutes, setRangeMinutes] = useState(24 * 60)
  const gradient = useId().replace(/:/g, '')

  useLayoutEffect(() => {
    const el = wrap.current
    if (!el) return
    setWidth(el.clientWidth)
    const ro = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    ro.observe(el)
    return () => ro.disconnect()
  }, [table])
  useEffect(() => setActive(null), [data, rangeMinutes])

  const visible = data.slice(-rangeMinutes)
  const totals = visible.map((p) => p.up + p.down)
  const totalUp = visible.reduce((sum, p) => sum + p.up, 0)
  const totalDown = visible.reduce((sum, p) => sum + p.down, 0)
  const max = Math.max(1024, ...totals)
  const plotW = Math.max(1, width - LEFT - RIGHT)
  const plotH = height - TOP - BOTTOM
  const x = (i: number) => LEFT + (i / Math.max(1, visible.length - 1)) * plotW
  const y = (v: number) => TOP + plotH - (v / max) * plotH
  const points = totals.map((v, i) => `${x(i).toFixed(2)},${y(v).toFixed(2)}`).join(' ')
  const area = visible.length ? `M${x(0)},${TOP + plotH}L${points.replaceAll(' ', 'L')}L${x(visible.length - 1)},${TOP + plotH}Z` : ''
  const line = visible.length ? `M${points.replaceAll(' ', 'L')}` : ''
  const time = (minute: number, long = false) => new Date(minute * 1000).toLocaleString(lng, long ? { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' } : { hour: '2-digit', minute: '2-digit' })
  const labelEvery = Math.max(1, Math.floor(visible.length / Math.max(1, Math.floor(plotW / 90))))
  const shown = active === null ? null : visible[active]
  const nonzero = visible.filter((p) => p.up + p.down > 0).reverse()

  const pick = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    setActive(Math.max(0, Math.min(visible.length - 1, Math.round(((e.clientX - rect.left - LEFT) / plotW) * (visible.length - 1)))))
  }
  const onKey = (e: KeyboardEvent<SVGSVGElement>) => {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault()
      setActive(Math.max(0, Math.min(visible.length - 1, (active ?? visible.length - 1) + (e.key === 'ArrowLeft' ? -1 : 1))))
    }
    if (e.key === 'Escape') setActive(null)
  }

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
        <div className="flex flex-wrap gap-x-4 gap-y-1 tabular-nums">
          <span>↓ {formatBytes(totalDown, lng)}</span>
          <span>↑ {formatBytes(totalUp, lng)}</span>
        </div>
        <button type="button" className="underline-offset-4 hover:text-foreground hover:underline" onClick={() => setTable((v) => !v)}>
          {table ? t('chart.asChart') : t('chart.asTable')}
        </button>
      </div>
      <div className="mb-3 flex items-center gap-1" aria-label={t('chart.zoom')}>
        <span className="mr-1 text-xs text-muted-foreground">{t('chart.zoom')}:</span>
        {([[30, 'range30'], [60, 'range60'], [360, 'range360'], [1440, 'range1440']] as const).map(([minutes, label]) => (
          <button
            key={minutes}
            type="button"
            aria-pressed={rangeMinutes === minutes}
            onClick={() => setRangeMinutes(minutes)}
            className={rangeMinutes === minutes ? 'rounded-md bg-accent px-2 py-1 text-xs font-medium text-accent-foreground' : 'rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground'}
          >
            {t(`chart.${label}`)}
          </button>
        ))}
      </div>
      {table ? (
        <div className="max-h-64 overflow-y-auto rounded-xl border">
          {nonzero.length === 0 ? <p className="p-4 text-sm text-muted-foreground">{t('chart.emptyMinute')}</p> : (
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-card text-left text-xs text-muted-foreground">
                <tr><th className="px-3 py-2 font-medium">{t('chart.minute')}</th><th className="px-3 py-2 text-right font-medium">↓</th><th className="px-3 py-2 text-right font-medium">↑</th></tr>
              </thead>
              <tbody>{nonzero.map((p) => (
                <tr key={p.minute} className="border-t tabular-nums"><td className="px-3 py-1.5">{time(p.minute, true)}</td><td className="px-3 py-1.5 text-right">{formatBytes(p.down, lng)}</td><td className="px-3 py-1.5 text-right">{formatBytes(p.up, lng)}</td></tr>
              ))}</tbody>
            </table>
          )}
        </div>
      ) : (
        <div ref={wrap} className="relative" style={{ height }}>
          {width > 0 && visible.length > 0 && (
            <svg width={width} height={height} role="img" tabIndex={0} aria-label={t('users.traffic24')} className="block touch-none rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring" onPointerMove={pick} onPointerLeave={() => setActive(null)} onKeyDown={onKey} onBlur={() => setActive(null)}>
              <defs><linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="var(--chart)" stopOpacity="0.38" /><stop offset="100%" stopColor="var(--chart)" stopOpacity="0.03" /></linearGradient></defs>
              {[0, max / 2, max].map((v) => <g key={v}><line x1={LEFT} x2={width} y1={y(v)} y2={y(v)} stroke="var(--border)" shapeRendering="crispEdges" /><text x={LEFT - 7} y={y(v)} dy="0.32em" textAnchor="end" className="fill-muted-foreground text-[10px] tabular-nums">{formatBytes(v, lng)}</text></g>)}
              <path d={area} fill={`url(#${gradient})`} />
              <path d={line} fill="none" stroke="var(--chart)" strokeWidth="1.6" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
              {visible.map((p, i) => (i % labelEvery === 0 || i === visible.length - 1) && <text key={p.minute} x={x(i)} y={height - 5} textAnchor={i === 0 ? 'start' : i === visible.length - 1 ? 'end' : 'middle'} className="fill-muted-foreground text-[10px]">{time(p.minute)}</text>)}
              {shown && active !== null && <g><line x1={x(active)} x2={x(active)} y1={TOP} y2={TOP + plotH} stroke="var(--chart)" strokeDasharray="3 3" /><circle cx={x(active)} cy={y(totals[active])} r="3.5" fill="var(--chart)" stroke="var(--card)" strokeWidth="2" /></g>}
            </svg>
          )}
          {shown && active !== null && (
            <div className="pointer-events-none absolute top-0 z-10 min-w-36 -translate-x-1/2 rounded-lg border bg-card px-3 py-2 text-xs shadow-md" style={{ left: Math.min(Math.max(x(active), 82), width - 82) }}>
              <div className="mb-1 text-muted-foreground tabular-nums">{time(shown.minute, true)}</div>
              <div className="font-semibold tabular-nums">{formatBytes(shown.up + shown.down, lng)}</div>
              <div className="mt-0.5 flex gap-3 text-muted-foreground tabular-nums"><span>↓ {formatBytes(shown.down, lng)}</span><span>↑ {formatBytes(shown.up, lng)}</span></div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
