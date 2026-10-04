import type { Reading, RoomGroup, RoomRef, Target } from './types';

export const PAGE_CONTAINER_CLASSES = 'mx-auto w-full max-w-5xl px-4 py-6';

/** 多宿舍分类调色板：与主色（琥珀橙）协调且彼此可区分。 */
export const PALETTE = [
  '#f97316',
  '#0ea5e9',
  '#10b981',
  '#8b5cf6',
  '#ef4444',
  '#eab308',
  '#ec4899',
  '#14b8a6',
  '#6366f1',
  '#84cc16',
] as const;

export const DAY_FILTERS = [
  { days: 7, label: '7 天' },
  { days: 30, label: '30 天' },
  { days: 90, label: '90 天' },
  { days: 0, label: '全部' },
] as const;

export const TABLE_PAGE_SIZES = [10, 20, 50, 100] as const;

export const ADMIN_KEY_STORE = 'elec-admin-key';
export const THEME_KEY = 'elec-theme';

export type TableView = 'raw' | 'daily' | 'recharge';

export function roomKey(r: Pick<RoomRef, 'campus' | 'building' | 'room'> | Reading): string {
  return [r.campus, r.building, r.room].join('|');
}

export function roomColorFor(r: Pick<RoomRef, 'campus' | 'building' | 'room'> | Reading): string {
  const text = roomKey(r);
  let hash = 0;
  for (let i = 0; i < text.length; i++) hash = (hash * 31 + text.charCodeAt(i)) | 0;
  const index = Math.abs(hash) % PALETTE.length;
  return PALETTE[index] ?? PALETTE[0];
}

export function targetLabel(t: Target): string {
  return t.label || `${t.campus}/${t.building}/${t.room}`;
}

export function labelForRow(r: Reading): string {
  return r.display_label || r.room_label || `${r.campus}/${r.building}/${r.room}`;
}

export function roomPath(r: RoomRef): string {
  return '/room/' + [r.campus, r.building, r.room].map(encodeURIComponent).join('/');
}

export function groupByRoom(rows: Reading[]): RoomGroup[] {
  const groups = new Map<string, RoomGroup>();
  for (const row of rows) {
    const key = roomKey(row);
    let group = groups.get(key);
    if (!group) {
      group = { key, label: labelForRow(row), rows: [] };
      groups.set(key, group);
    }
    group.rows.push(row);
  }
  const list = [...groups.values()];
  // 顺序只取决于数据本身，避免历史房间/裁剪配置导致顺序漂移。
  list.sort((a, b) => a.key.localeCompare(b.key));
  return list;
}
