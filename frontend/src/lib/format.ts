/** 格式化与余额分级：与旧版 app.js 的展示口径保持一致。 */

export function fmt(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  return Number(value).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

export function parseTs(ts: string): Date {
  return new Date(ts.replace(' ', 'T'));
}

/** 2026-10-03 22:24 */
export function fullTs(ts: string): string {
  const d = parseTs(ts);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

export type BalanceLevel = '' | 'good' | 'warning' | 'serious' | 'critical';

export interface BalanceStatus {
  level: BalanceLevel;
  text: string;
}

/** 低电量分级：>=30 充足，10-30 偏低，5-10 不足，<5 欠费/临界。 */
export function balanceStatus(value: number | null | undefined): BalanceStatus {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return { level: '', text: '暂无数据' };
  }
  if (value < 0) return { level: 'critical', text: '已欠费' };
  if (value < 5) return { level: 'critical', text: '即将断电' };
  if (value < 10) return { level: 'serious', text: '余额不足' };
  if (value < 30) return { level: 'warning', text: '余额偏低' };
  return { level: 'good', text: '余额充足' };
}

/** 最新的读数变化量（同一宿舍内相邻两条）。 */
export function balanceDelta(rows: { surplus_charge: number | null }[]): {
  text: string;
  tone: 'up' | 'down' | 'flat';
} {
  if (rows.length < 2) return { text: '', tone: 'flat' };
  const cur = rows[rows.length - 1]?.surplus_charge;
  const prev = rows[rows.length - 2]?.surplus_charge;
  if (cur === null || cur === undefined || prev === null || prev === undefined) {
    return { text: '', tone: 'flat' };
  }
  const d = cur - prev;
  if (Math.abs(d) < 1e-9) return { text: '', tone: 'flat' };
  return { text: `${d > 0 ? '+' : ''}${fmt(d)} kWh`, tone: d > 0 ? 'up' : 'down' };
}
