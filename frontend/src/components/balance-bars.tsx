import * as React from 'react';
import { cn } from '@/lib/utils';
import { balanceStatus, fmt, type BalanceLevel } from '@/lib/format';
import type { Reading } from '@/lib/types';

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

/** 每个点代表的时间跨度：从小到大取第一个能让点数 ≤ target 的档位。 */
const BUCKET_STEPS = [
  HOUR,
  2 * HOUR,
  3 * HOUR,
  6 * HOUR,
  12 * HOUR,
  DAY,
  2 * DAY,
  3 * DAY,
  7 * DAY,
  14 * DAY,
  30 * DAY,
] as const;

export const LEVEL_BG: Record<BalanceLevel, string> = {
  good: 'bg-status-operational',
  warning: 'bg-status-degraded',
  serious: 'bg-status-serious',
  critical: 'bg-status-down',
  '': 'bg-status-unknown-bg',
};

const LEVEL_LABEL: Record<BalanceLevel, string> = {
  good: '余额充足',
  warning: '余额偏低',
  serious: '余额不足',
  critical: '临界/欠费',
  '': '无数据',
};

export interface BalanceBucket {
  start: number;
  end: number;
  /** 该时段内的最低余额，用于颜色映射；null = 无数据。 */
  min: number | null;
  level: BalanceLevel;
}

export function pickBucketMs(rangeMs: number, target = 64): number {
  const span = Math.max(1, rangeMs);
  for (const step of BUCKET_STEPS) {
    if (span / step <= target) return step;
  }
  return BUCKET_STEPS[BUCKET_STEPS.length - 1]!;
}

/** 把一段读数按自适应时间桶聚合，取每桶最低余额映射到状态色。 */
export function buildBalanceBuckets(
  rows: Reading[],
  start: number,
  end: number,
  target = 64,
): BalanceBucket[] {
  const span = Math.max(1, end - start);
  const bucketMs = pickBucketMs(span, target);
  const count = Math.max(1, Math.ceil(span / bucketMs));
  const buckets: BalanceBucket[] = Array.from({ length: count }, (_, index) => ({
    start: start + index * bucketMs,
    end: Math.min(end, start + (index + 1) * bucketMs),
    min: null,
    level: '',
  }));
  for (const row of rows) {
    const value = row.surplus_charge;
    if (value === null || value === undefined || Number.isNaN(value)) continue;
    const time = new Date(row.ts.replace(' ', 'T')).getTime();
    if (Number.isNaN(time) || time < start || time > end) continue;
    const index = Math.min(count - 1, Math.max(0, Math.floor((time - start) / bucketMs)));
    const bucket = buckets[index];
    if (bucket && (bucket.min === null || value < bucket.min)) bucket.min = value;
  }
  for (const bucket of buckets) {
    bucket.level = bucket.min === null ? '' : balanceStatus(bucket.min).level;
  }
  return buckets;
}

function formatRange(start: number, end: number): string {
  const from = new Date(start);
  const to = new Date(Math.max(start, end - 1));
  const p = (n: number) => String(n).padStart(2, '0');
  const day = (d: Date) => `${p(d.getMonth() + 1)}-${p(d.getDate())}`;
  const time = (d: Date) => `${p(d.getHours())}:${p(d.getMinutes())}`;
  if (day(from) === day(to)) return `${day(from)} ${time(from)}–${time(to)}`;
  return `${day(from)}–${day(to)}`;
}

/** 一行状态条：每个点一个时间桶，颜色映射该时段最低余额。 */
export function BalanceBars({ buckets, className }: { buckets: BalanceBucket[]; className?: string }) {
  const ref = React.useRef<HTMLDivElement>(null);
  const [active, setActive] = React.useState<{ index: number; x: number } | null>(null);

  if (buckets.length === 0) return null;

  function handleMove(event: React.MouseEvent<HTMLDivElement>) {
    const rect = ref.current?.getBoundingClientRect();
    if (!rect || rect.width === 0) return;
    const x = Math.max(0, Math.min(rect.width, event.clientX - rect.left));
    const index = Math.min(buckets.length - 1, Math.floor((x / rect.width) * buckets.length));
    setActive({ index, x });
  }

  const bucket = active ? buckets[active.index] : null;
  const width = ref.current?.clientWidth ?? 0;
  const tipX = active ? Math.max(76, Math.min(Math.max(76, width - 76), active.x)) : 0;

  return (
    <div className={cn('relative', className)}>
      <div
        ref={ref}
        className="flex h-4 items-stretch gap-0.5"
        onMouseMove={handleMove}
        onMouseLeave={() => setActive(null)}
      >
        {buckets.map((item, index) => (
          <span
            key={index}
            className={cn('min-w-0 flex-1 rounded-[2px]', LEVEL_BG[item.level])}
            aria-hidden="true"
          />
        ))}
      </div>

      {bucket && (
        <div
          className="pointer-events-none absolute bottom-full z-20 mb-2 -translate-x-1/2 rounded-md border border-border bg-popover px-2.5 py-1.5 text-xs whitespace-nowrap shadow-md"
          style={{ left: tipX }}
        >
          <div className="font-medium text-popover-foreground">
            {LEVEL_LABEL[bucket.level]}
            {bucket.min !== null ? ` · 最低 ${fmt(bucket.min)} kWh` : ''}
          </div>
          <div className="text-muted-foreground">{formatRange(bucket.start, bucket.end)}</div>
        </div>
      )}
    </div>
  );
}

/** 状态条颜色图例。 */
export function BalanceLegend({ className }: { className?: string }) {
  const items: BalanceLevel[] = ['good', 'warning', 'serious', 'critical', ''];
  return (
    <div
      className={cn(
        'flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground',
        className,
      )}
    >
      {items.map((level) => (
        <span key={level} className="inline-flex items-center gap-1.5">
          <span className={cn('size-2.5 rounded-[2px]', LEVEL_BG[level])} aria-hidden="true" />
          {LEVEL_LABEL[level]}
        </span>
      ))}
    </div>
  );
}
