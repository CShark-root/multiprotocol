import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import type { DayTraffic } from '@/lib/api'
import { formatBytes } from '@/lib/utils'

// One series (bytes per day) as columns: thin bars with a rounded data end,
// a hairline grid, a tooltip on hover and keyboard focus, and a table view
// so no value depends on hovering.

const AXIS_W = 52 // room for the y tick labels
const AXIS_H = 22 // room for the date labels
const TOP = 8
const BAR_MAX = 24
const GAP = 2

function niceStep(raw: number): number {
  const p = 10 ** Math.floor(Math.log10(raw))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= raw) return m * p
  return 10 * p
}

/** Ticks at round values of the unit the maximum is shown in (MB, GB…). */
function yTicks(max: number): number[] {
  if (max <= 0) return [0]
  const unit = 1024 ** Math.min(4, Math.floor(Math.log(max) / Math.log(1024)))
  const step = niceStep(max / unit / 3) * unit
  const ticks = []
  for (let v = 0; v < max + step; v += step) ticks.push(v)
  return ticks
}

function barPath(x: number, y: number, w: number, h: number): string {
  const r = Math.min(4, w / 2, h)
  // Rounded at the data end, square at the baseline.
  return `M${x},${y + h}V${y + r}Q${x},${y} ${x + r},${y}H${x + w - r}Q${x + w},${y} ${x + w},${y + r}V${y + h}Z`
}

export function TrafficChart({ data, height = 180 }: { data: DayTraffic[]; height?: number }) {
  const { t, i18n } = useTranslation()
  const lng = i18n.language
  const wrap = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [active, setActive] = useState<number | null>(null)
  const [table, setTable] = useState(false)

  useLayoutEffect(() => {
    const el = wrap.current
    if (!el) return
    setWidth(el.clientWidth)
    const ro = new ResizeObserver(([e]) => setWidth(e.contentRect.width))
    ro.observe(el)
    return () => ro.disconnect()
  }, [table])
  useEffect(() => setActive(null), [data])

  const dayLabel = (day: string, long = false) =>
    new Date(day + 'T00:00:00').toLocaleDateString(lng, long ? { weekday: 'short', day: 'numeric', month: 'long' } : { day: 'numeric', month: 'short' })

  const total = data.reduce((s, d) => s + d.bytes, 0)
  const max = Math.max(0, ...data.map((d) => d.bytes))
  const ticks = yTicks(max)
  const top = ticks[ticks.length - 1] || 1
  const plotW = Math.max(0, width - AXIS_W)
  const plotH = height - AXIS_H - TOP
  const band = data.length ? plotW / data.length : 0
  const barW = Math.max(1, Math.min(BAR_MAX, band - GAP))
  const y = (v: number) => TOP + plotH - (v / top) * plotH
  // Label about every week, always including the last day.
  const labelEvery = Math.max(1, Math.ceil(data.length / Math.max(1, Math.floor(plotW / 64))))

  function onKey(e: KeyboardEvent) {
    if (!data.length) return
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault()
      const cur = active ?? data.length
      setActive(Math.max(0, Math.min(data.length - 1, cur + (e.key === 'ArrowLeft' ? -1 : 1))))
    } else if (e.key === 'Escape') setActive(null)
  }

  const shown = active !== null ? data[active] : null
  const tipLeft = active !== null ? AXIS_W + band * active + band / 2 : 0

  return (
    <div>
      <div className="mb-3 flex items-baseline justify-between gap-3">
        <div className="text-sm text-muted-foreground">{t('chart.total', { value: formatBytes(total, lng) })}</div>
        <button
          type="button"
          className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
          onClick={() => setTable((v) => !v)}
        >
          {table ? t('chart.asChart') : t('chart.asTable')}
        </button>
      </div>

      {table ? (
        <div className="max-h-64 overflow-y-auto rounded-xl border">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-card">
              <tr className="text-left text-xs text-muted-foreground">
                <th className="px-3 py-2 font-medium">{t('chart.day')}</th>
                <th className="px-3 py-2 text-right font-medium">{t('chart.traffic')}</th>
              </tr>
            </thead>
            <tbody>
              {[...data].reverse().map((d) => (
                <tr key={d.day} className="border-t">
                  <td className="px-3 py-1.5">{dayLabel(d.day, true)}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums">{formatBytes(d.bytes, lng)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div ref={wrap} className="relative" style={{ height }}>
          {width > 0 && (
            <svg
              width={width}
              height={height}
              role="img"
              tabIndex={0}
              aria-label={t('chart.aria', { days: data.length, total: formatBytes(total, lng) })}
              className="block rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring"
              onKeyDown={onKey}
              onBlur={() => setActive(null)}
              onPointerLeave={() => setActive(null)}
            >
              {ticks.map((v) => (
                <g key={v}>
                  <line x1={AXIS_W} x2={width} y1={y(v)} y2={y(v)} stroke="var(--border)" strokeWidth={1} shapeRendering="crispEdges" />
                  <text x={AXIS_W - 8} y={y(v)} dy="0.32em" textAnchor="end" className="fill-muted-foreground text-[11px] tabular-nums">
                    {formatBytes(v, lng)}
                  </text>
                </g>
              ))}
              {data.map((d, i) => {
                const x = AXIS_W + band * i + (band - barW) / 2
                const h = (d.bytes / top) * plotH
                const last = i === data.length - 1
                return (
                  <g key={d.day}>
                    {h > 0 && (
                      <path
                        d={barPath(x, TOP + plotH - Math.max(h, 1), barW, Math.max(h, 1))}
                        fill="var(--chart)"
                        opacity={active === null || active === i ? 1 : 0.55}
                      />
                    )}
                    {(last || (data.length - 1 - i) % labelEvery === 0) && (
                      <text x={AXIS_W + band * i + band / 2} y={height - 6} textAnchor={last ? 'end' : 'middle'} className="fill-muted-foreground text-[11px]">
                        {dayLabel(d.day)}
                      </text>
                    )}
                    {/* The hit target is the whole column, not just the painted bar. */}
                    <rect x={AXIS_W + band * i} y={TOP} width={band} height={plotH} fill="transparent" onPointerEnter={() => setActive(i)} />
                  </g>
                )
              })}
            </svg>
          )}
          {shown && (
            <div
              className="pointer-events-none absolute z-10 -translate-x-1/2 whitespace-nowrap rounded-lg border bg-card px-2.5 py-1.5 text-xs shadow-sm"
              style={{ left: Math.min(Math.max(tipLeft, 70), width - 70), top: 0 }}
            >
              <div className="text-sm font-semibold tabular-nums">{formatBytes(shown.bytes, lng)}</div>
              <div className="text-muted-foreground">{dayLabel(shown.day, true)}</div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
