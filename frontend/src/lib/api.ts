import type {
  DailyStat,
  PublicConfig,
  PowerPoint,
  Reading,
  RechargeEvent,
  RoomRef,
} from './types';

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

async function toJSON<T>(response: Response): Promise<T> {
  const body = (await response.json().catch(() => ({}))) as T & { error?: string };
  if (!response.ok) {
    throw new ApiError(body?.error || `HTTP ${response.status}`, response.status);
  }
  return body;
}

/** 离线兜底：Service Worker 未接管时（首访/被禁用）读 Cache Storage 里的公开响应。 */
async function cachedJSON<T>(url: string): Promise<T | null> {
  if (typeof caches === 'undefined') return null;
  try {
    const cached = await caches.match(url);
    if (!cached) return null;
    return (await cached.json()) as T;
  } catch {
    return null;
  }
}

/**
 * 发起请求；真正的网络故障（非 HTTP 错误）时回退到缓存，与旧版
 * loadCachedConfig/loadCachedReadings 行为一致。
 */
async function requestJSON<T>(url: string, init: RequestInit, cacheUrl?: string): Promise<T> {
  try {
    return await toJSON<T>(await fetch(url, init));
  } catch (error) {
    if (error instanceof ApiError) throw error;
    const cached = await cachedJSON<T>(cacheUrl ?? url);
    if (cached !== null) return cached;
    throw error;
  }
}

export interface RoomQuery {
  days?: number;
  room?: RoomRef | null;
}

function roomParams(q: RoomQuery): URLSearchParams {
  const params = new URLSearchParams();
  if (q.days) params.set('days', String(q.days));
  if (q.room) {
    params.set('campus', q.room.campus);
    params.set('building', q.room.building);
    params.set('room', q.room.room);
  }
  return params;
}

function withQuery(path: string, params: URLSearchParams): string {
  const suffix = params.toString();
  return suffix ? `${path}?${suffix}` : path;
}

/** 公共读数：主页隐藏时的聚合请求会带上管理密钥，由服务端决定是否放行。 */
export async function getReadings(q: RoomQuery, key: string, signal?: AbortSignal): Promise<Reading[]> {
  const headers = new Headers();
  const aggregate = !q.room;
  if (aggregate && key) headers.set('Authorization', `Bearer ${key}`);
  const url = withQuery('/api/readings', roomParams(q));
  const data = await requestJSON<Reading[]>(
    url,
    { cache: 'no-store', headers, signal },
    aggregate ? '/api/readings' : url,
  );
  return Array.isArray(data) ? data : [];
}

export async function getDaily(q: RoomQuery, signal?: AbortSignal): Promise<DailyStat[]> {
  const url = withQuery('/api/daily', roomParams(q));
  const data = await requestJSON<DailyStat[]>(url, { cache: 'no-store', signal });
  return Array.isArray(data) ? data : [];
}

export async function getRecharges(q: RoomQuery, signal?: AbortSignal): Promise<RechargeEvent[]> {
  const url = withQuery('/api/recharges', roomParams(q));
  const data = await requestJSON<RechargeEvent[]>(url, { cache: 'no-store', signal });
  return Array.isArray(data) ? data : [];
}

export async function getPower(
  q: RoomQuery,
  bucketMinutes: number,
  signal?: AbortSignal,
): Promise<PowerPoint[]> {
  const params = roomParams(q);
  params.set('bucket', String(bucketMinutes));
  const url = withQuery('/api/power', params);
  const data = await requestJSON<PowerPoint[]>(url, { cache: 'no-store', signal });
  return Array.isArray(data) ? data : [];
}

export interface ConfigQuery {
  room?: RoomRef | null;
  key?: string;
  /** 管理视图：列出全部宿舍（含隐藏项）。 */
  admin?: boolean;
  /** 查询某个宿舍是否已存在/是否被隐藏。 */
  lookup?: RoomRef;
}

export async function getConfig(q: ConfigQuery = {}, signal?: AbortSignal): Promise<PublicConfig> {
  const params = new URLSearchParams();
  const filter = q.lookup ?? q.room;
  if (filter) {
    params.set('campus', filter.campus);
    params.set('building', filter.building);
    params.set('room', filter.room);
  }
  if (q.admin) params.set('admin', '1');
  const headers = new Headers();
  if (q.key) headers.set('Authorization', `Bearer ${q.key}`);
  const url = withQuery('/api/config', params);
  return requestJSON<PublicConfig>(url, { cache: 'no-store', headers, signal }, '/api/config');
}

export async function verifyAdminKey(key: string): Promise<boolean> {
  const response = await fetch('/api/admin/verify', {
    method: 'POST',
    cache: 'no-store',
    headers: { Authorization: `Bearer ${key}` },
  });
  if (response.status === 401) return false;
  await toJSON<{ ok: boolean }>(response);
  return true;
}
