/** 与后端 internal/web 的 JSON 契约一一对应。 */

export interface Target {
  feeitemid: number;
  appId: number;
  campus: string;
  building: string;
  room: string;
  label: string;
  show_in_web?: boolean | null;
  poll_interval_minutes?: number | null;
  notify_mode?: string;
  notify_time?: string;
  webhook?: unknown;
}

export interface Reading {
  ts: string;
  epoch: number;
  room_label: string;
  display_label?: string;
  surplus_charge: number | null;
  total_usage: number | null;
  consumption_kwh: number | null;
  recharge_kwh: number | null;
  power_kw: number | null;
  show?: Record<string, string>;
  campus: string;
  building: string;
  room: string;
}

export interface DailyStat {
  day: string;
  consumption_kwh: number | null;
  recharge_kwh: number | null;
  avg_power_kw: number | null;
  samples: number;
  surplus_end: number | null;
}

export interface RechargeEvent {
  ts: string;
  epoch: number;
  recharge_kwh: number | null;
  surplus_after: number | null;
  interval_consumption_kwh: number | null;
}

export interface PowerPoint {
  ts: string;
  epoch: number;
  window_seconds: number | null;
  consumption_kwh: number | null;
  power_kw: number | null;
}

export type CollectButtonMode = 'always' | 'when_logged_in' | 'never';

export interface PublicConfig {
  targets: Target[] | null;
  defaults: { feeitemid: number; appId: number };
  admin_auth_required: boolean;
  show_homepage: boolean;
  guest_add_allowed: boolean;
  /** 「立即采集」按钮显隐策略。 */
  show_collect_button?: CollectButtonMode;
  target_exists?: boolean;
  target_hidden?: boolean;
}

/** SSE `reading` 事件负载（字段与 db.ReadingRow 对齐）。 */
export interface ReadingEvent {
  campus: string;
  building: string;
  room: string;
  room_label: string;
  display_label?: string;
  surplus_charge: number | null;
  ts: string;
  epoch: number;
}

export interface CollectStatus {
  job_id: string;
  state: 'queued' | 'running' | 'cancelling' | 'done' | 'failed' | 'cancelled';
  requested: number;
  completed: number;
  success: number;
  failed: number;
  percent?: number;
  current?: { label?: string } | null;
  error?: string;
  results?: Array<{ error?: string }>;
}

export interface RoomRef {
  campus: string;
  building: string;
  room: string;
  label?: string;
}

export interface RoomGroup {
  key: string;
  label: string;
  rows: Reading[];
}
