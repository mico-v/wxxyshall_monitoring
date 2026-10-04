import * as React from 'react';
import { cn } from '@/lib/utils';
import { areaPath, linearScale, niceLinearTicks, smoothPath, timeTicks, type Point } from '@/lib/chart';

export interface SeriesPoint {
  /** 毫秒时间戳 */
  x: number;
  y: number;
}

interface TimeSeriesChartProps {
  data: SeriesPoint[];
  /** 数值格式化（tooltip / 末端标签）。 */
  formatValue: (value: number) => string;
  /** 坐标轴刻度格式化；默认复用 formatValue。 */
  formatAxis?: (value: number) => string;
  color?: string;
  height?: number;
  compact?: boolean;
  emptyText?: string;
  unit?: string;
  className?: string;
  'aria-label'?: string;
}

const VB = 100;
const PADDING_TOP = 4;

function formatTickTime(ms: number, span: number): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, '0');
  if (span >= 2 * 24 * 3600_000) return `${p(d.getMonth() + 1)}-${p(d.getDate())}`;
  return `${p(d.getHours())}:${p(d.getMinutes())}`;
}

function nearestIndex(data: SeriesPoint[], x: number): number {
  let lo = 0;
  let hi = data.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    const point = data[mid];
    if (point && point.x < x) lo = mid + 1;
    else hi = mid;
  }
  const prev = data[lo - 1];
  const cur = data[lo];
  if (prev && cur && Math.abs(prev.x - x) <= Math.abs(cur.x - x)) return lo - 1;
  return lo;
}

/**
 * 单序列时间折线图。布局与 flarewatch 的 latency-chart 相同：SVG 铺满绘图区，
 * 文字与 tooltip 用 HTML 覆盖层按百分比定位，保证任何宽度下都清晰。
 */
export function TimeSeriesChart({
  data,
  formatValue,
  formatAxis,
  color = 'var(--primary)',
  height = 240,
  compact = false,
  emptyText = '暂无数据',
  unit,
  className,
  'aria-label': ariaLabel,
}: TimeSeriesChartProps) {
  const plotRef = React.useRef<HTMLDivElement>(null);
  const [activeIndex, setActiveIndex] = React.useState<number | null>(null);
  const gradientId = React.useId().replace(/[^a-zA-Z0-9_-]/g, '');

  if (data.length === 0) {
    return (
      <div
        className={cn(
          'flex w-full items-center justify-center rounded-md border border-dashed border-border text-xs text-muted-foreground',
          className,
        )}
        style={{ height }}
      >
        {emptyText}
      </div>
    );
  }

  const times = data.map((d) => d.x);
  const values = data.map((d) => d.y);
  const xDomain: [number, number] = [Math.min(...times), Math.max(...times)];
  const span = xDomain[1] - xDomain[0];
  const rawMin = Math.min(0, ...values);
  const rawMax = Math.max(...values);
  const { ticks: yTicks, max: yMax } = niceLinearTicks(rawMax, rawMin);
  const yMin = rawMin < 0 ? -yMax : 0;
  const yAxisWidth = compact ? 40 : 52;
  const xAxisHeight = compact ? 16 : 20;
  const labelSize = compact ? 9 : 10;

  const { ticks: xTicks, coarseStep } = timeTicks(xDomain[0], xDomain[1]);
  const axisFormat = formatAxis ?? formatValue;
  const xScale = linearScale(xDomain, [0, VB]);
  const yScale = linearScale([yMin, yMax], [VB, 0]);

  const points: Point[] = data.map((d) => ({ x: xScale(d.x), y: yScale(d.y) }));
  const linePath = smoothPath(points);
  const fillPath = areaPath(points, yScale(yMin));

  const active = activeIndex === null ? null : data[activeIndex];
  const activePoint = activeIndex === null ? null : points[activeIndex];
  const fracX = (x: number) => xScale(x) / VB;
  const fracY = (y: number) => yScale(y) / VB;
  const xTier = (tick: number) => (tick % coarseStep === 0 ? 0 : 1);

  function handlePointer(event: React.PointerEvent<HTMLDivElement>) {
    const rect = plotRef.current?.getBoundingClientRect();
    if (!rect) return;
    const frac = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
    setActiveIndex(nearestIndex(data, xDomain[0] + frac * span));
  }

  const last = data[data.length - 1];

  return (
    <div
      className={cn('relative touch-pan-y select-none', className)}
      style={{ height }}
      role="img"
      aria-label={ariaLabel ?? '时间序列曲线'}
      onPointerDown={handlePointer}
      onPointerMove={handlePointer}
      onPointerUp={() => setActiveIndex(null)}
      onPointerLeave={() => setActiveIndex(null)}
      onPointerCancel={() => setActiveIndex(null)}
    >
      <div
        ref={plotRef}
        className="absolute"
        style={{
          left: yAxisWidth,
          right: 8,
          top: PADDING_TOP,
          bottom: xAxisHeight,
          containerType: 'inline-size',
        }}
      >
        <svg
          viewBox={`0 0 ${VB} ${VB}`}
          preserveAspectRatio="none"
          className="absolute inset-0 h-full w-full overflow-visible"
          aria-hidden="true"
        >
          <defs>
            <linearGradient id={gradientId} gradientUnits="userSpaceOnUse" x1={0} y1={0} x2={0} y2={VB}>
              <stop offset={0} stopColor={color} stopOpacity={0.2} />
              <stop offset={1} stopColor={color} stopOpacity={0.02} />
            </linearGradient>
          </defs>

          <path d={fillPath} fill={`url(#${gradientId})`} />

          {yTicks.map((tick) => (
            <line
              key={`h-${tick}`}
              x1={0}
              x2={VB}
              y1={yScale(tick)}
              y2={yScale(tick)}
              stroke="var(--border)"
              vectorEffect="non-scaling-stroke"
            />
          ))}
          {xTicks.map((tick) => (
            <line
              key={`v-${tick}`}
              x1={xScale(tick)}
              x2={xScale(tick)}
              y1={0}
              y2={VB}
              stroke="var(--border)"
              strokeDasharray="2 3"
              vectorEffect="non-scaling-stroke"
            />
          ))}

          {activePoint && (
            <line
              x1={activePoint.x}
              x2={activePoint.x}
              y1={0}
              y2={VB}
              stroke="var(--muted-foreground)"
              vectorEffect="non-scaling-stroke"
            />
          )}

          <path
            d={linePath}
            fill="none"
            stroke={color}
            strokeWidth={1.5}
            strokeLinejoin="round"
            strokeLinecap="round"
            vectorEffect="non-scaling-stroke"
          />
        </svg>

        {activePoint && active && (
          <span
            className="pointer-events-none absolute size-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full"
            style={{ left: `${fracX(active.x) * 100}%`, top: `${fracY(active.y) * 100}%`, background: color }}
          />
        )}

        {yTicks.map((tick) => (
          <span
            key={`yl-${tick}`}
            className="pointer-events-none absolute right-full -translate-y-1/2 pr-1 whitespace-nowrap text-muted-foreground tabular-nums"
            style={{ top: `${fracY(tick) * 100}%`, fontSize: labelSize }}
          >
            {axisFormat(tick)}
          </span>
        ))}

        {xTicks.map((tick) => (
          <span
            key={`xl-${tick}`}
            data-tier={xTier(tick)}
            className={cn(
              'pointer-events-none absolute top-full mt-1 -translate-x-1/2 whitespace-nowrap text-muted-foreground',
              xTier(tick) === 1 && 'hidden @min-[420px]:inline',
            )}
            style={{ left: `${fracX(tick) * 100}%`, fontSize: labelSize }}
          >
            {formatTickTime(tick, span)}
          </span>
        ))}

        {active && activePoint && (
          <div
            className="pointer-events-none absolute z-10 rounded border border-border bg-popover px-2 py-1.5 text-xs shadow-sm"
            style={
              fracX(active.x) > 0.5
                ? { right: `${(1 - fracX(active.x)) * 100}%`, marginRight: 8, top: 0 }
                : { left: `${fracX(active.x) * 100}%`, marginLeft: 8, top: 0 }
            }
          >
            <div className="font-medium text-popover-foreground">
              {formatValue(active.y)}
              {unit ? ` ${unit}` : ''}
            </div>
            <div className="text-muted-foreground">
              {new Date(active.x).toLocaleString('zh-CN', { hour12: false })}
            </div>
          </div>
        )}
      </div>

      {last && !compact && (
        <span
          className="pointer-events-none absolute top-0 right-0 rounded-full px-2 py-0.5 text-xs font-semibold"
          style={{ color }}
        >
          {formatValue(last.y)}
          {unit ? ` ${unit}` : ''}
        </span>
      )}
    </div>
  );
}
