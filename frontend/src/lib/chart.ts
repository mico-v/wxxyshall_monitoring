/**
 * 图表刻度与路径工具。linearScale/niceLinearTicks/timeTicks 移植自 flarewatch
 * （MIT，apps/status-page/src/lib/chart-scale.ts 与 chart-ticks.ts），保证刻度
 * 落在"整数"上；曲线用单调三次插值，避免过冲。
 */

export interface NiceTicks {
  ticks: number[];
  max: number;
}

export function linearScale(
  [d0, d1]: [number, number],
  [r0, r1]: [number, number],
): (value: number) => number {
  const span = d1 - d0 || 1;
  return (value: number) => r0 + ((value - d0) / span) * (r1 - r0);
}

const E10 = Math.sqrt(50);
const E5 = Math.sqrt(10);
const E2 = Math.sqrt(2);

function tickSpec(start: number, stop: number, count: number): [number, number, number] {
  const rawStep = (stop - start) / Math.max(0, count);
  const power = Math.floor(Math.log10(rawStep));
  const error = rawStep / 10 ** power;
  const factor = error >= E10 ? 10 : error >= E5 ? 5 : error >= E2 ? 2 : 1;
  let i1: number;
  let i2: number;
  let inc: number;
  if (power < 0) {
    inc = 10 ** -power / factor;
    i1 = Math.round(start * inc);
    i2 = Math.round(stop * inc);
    if (i1 / inc < start) ++i1;
    if (i2 / inc > stop) --i2;
    inc = -inc;
  } else {
    inc = 10 ** power * factor;
    i1 = Math.round(start / inc);
    i2 = Math.round(stop / inc);
    if (i1 * inc < start) ++i1;
    if (i2 * inc > stop) --i2;
  }
  if (i2 < i1 && count >= 0.5 && count < 2) return tickSpec(start, stop, count * 2);
  return [i1, i2, inc];
}

function tickIncrement(start: number, stop: number, count: number): number {
  return tickSpec(start, stop, count)[2];
}

function ticks(start: number, stop: number, count: number): number[] {
  if (!(count > 0)) return [];
  if (start === stop) return [start];
  const [i1, i2, inc] = tickSpec(start, stop, count);
  if (!(i2 >= i1)) return [];
  const n = i2 - i1 + 1;
  return Array.from({ length: n }, (_, i) => (inc < 0 ? (i1 + i) / -inc : (i1 + i) * inc));
}

function niceMax(maxValue: number, count: number): number {
  if (!(maxValue > 0)) return maxValue;
  let stop = maxValue;
  let prestep: number | undefined;
  let maxIter = 10;
  while (maxIter-- > 0) {
    const step = tickIncrement(0, stop, count);
    if (step === prestep) break;
    if (step > 0) stop = Math.ceil(stop / step) * step;
    else if (step < 0) stop = Math.floor(stop * step) / step;
    else break;
    prestep = step;
  }
  return stop;
}

/** 与 d3 scaleLinear().domain([0, max]).nice().ticks(5) 一致（max 可为负→0 起始）。 */
export function niceLinearTicks(maxValue: number, minValue = 0): NiceTicks {
  if (minValue < 0) {
    // 余额可能为负：以 0 对称地扩展到两侧。
    const span = Math.max(Math.abs(minValue), maxValue, 1);
    const max = niceMax(span, 10);
    return { ticks: ticks(-max, max, 5), max };
  }
  const max = niceMax(maxValue, 10);
  return { ticks: ticks(0, max, 5), max };
}

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

const CANONICAL_STEPS = [
  MIN,
  2 * MIN,
  5 * MIN,
  10 * MIN,
  15 * MIN,
  30 * MIN,
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
];

const FINE_TARGET = 7;

function nearestCanonical(idealMs: number): number {
  let best = MIN;
  for (const step of CANONICAL_STEPS) {
    if (Math.abs(Math.log(step / idealMs)) < Math.abs(Math.log(best / idealMs))) best = step;
  }
  return best;
}

function ticksAtStep(domainMin: number, domainMax: number, stepMs: number): number[] {
  if (stepMs <= 0) return [];
  const start = Math.floor(domainMin / stepMs) * stepMs;
  const end = Math.ceil(domainMax / stepMs) * stepMs;
  const out: number[] = [];
  for (let v = start; v <= end; v += stepMs) {
    if (v >= domainMin && v <= domainMax) out.push(v);
  }
  return out;
}

function coarseMultiple(fineStep: number): number {
  for (const step of CANONICAL_STEPS) {
    if (step >= 2 * fineStep && step % fineStep === 0) return step;
  }
  return fineStep * 2;
}

export function timeTicks(domainMin: number, domainMax: number): {
  ticks: number[];
  coarseStep: number;
} {
  const fineStep = nearestCanonical((domainMax - domainMin) / FINE_TARGET);
  return {
    ticks: ticksAtStep(domainMin, domainMax, fineStep),
    coarseStep: coarseMultiple(fineStep),
  };
}

export interface Point {
  x: number;
  y: number;
}

/** 单调三次插值路径（Fritsch–Carlson），不会像 Catmull-Rom 那样过冲。 */
export function smoothPath(points: Point[]): string {
  const n = points.length;
  if (n === 0) return '';
  const first = points[0];
  if (!first) return '';
  if (n === 1) return `M ${first.x} ${first.y}`;
  if (n === 2) {
    const second = points[1];
    if (!second) return `M ${first.x} ${first.y}`;
    return `M ${first.x} ${first.y} L ${second.x} ${second.y}`;
  }

  const dx: number[] = [];
  const dy: number[] = [];
  const slope: number[] = [];
  for (let i = 0; i < n - 1; i++) {
    const a = points[i];
    const b = points[i + 1];
    if (!a || !b) continue;
    const h = b.x - a.x;
    dx.push(h);
    dy.push(b.y - a.y);
    slope.push(h === 0 ? 0 : (b.y - a.y) / h);
  }

  const m: number[] = new Array(n).fill(0);
  m[0] = slope[0] ?? 0;
  m[n - 1] = slope[n - 2] ?? 0;
  for (let i = 1; i < n - 1; i++) {
    const s0 = slope[i - 1];
    const s1 = slope[i];
    if (s0 === undefined || s1 === undefined) {
      m[i] = 0;
      continue;
    }
    if (s0 * s1 <= 0) {
      m[i] = 0;
    } else {
      const h0 = dx[i - 1] ?? 1;
      const h1 = dx[i] ?? 1;
      const w1 = 2 * h1 + h0;
      const w2 = h1 + 2 * h0;
      m[i] = (w1 + w2) / (w1 / s0 + w2 / s1);
    }
  }

  let d = `M ${first.x} ${first.y}`;
  for (let i = 0; i < n - 1; i++) {
    const a = points[i];
    const b = points[i + 1];
    if (!a || !b) continue;
    const h = dx[i] ?? 0;
    const c1x = a.x + h / 3;
    const c1y = a.y + ((m[i] ?? 0) * h) / 3;
    const c2x = b.x - h / 3;
    const c2y = b.y - ((m[i + 1] ?? 0) * h) / 3;
    d += ` C ${c1x} ${c1y}, ${c2x} ${c2y}, ${b.x} ${b.y}`;
  }
  return d;
}

export function areaPath(points: Point[], baseY: number): string {
  const line = smoothPath(points);
  const first = points[0];
  const last = points[points.length - 1];
  if (!line || !first || !last) return '';
  return `${line} L ${last.x} ${baseY} L ${first.x} ${baseY} Z`;
}
