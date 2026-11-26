import { useQuery, type QueryKey } from '@tanstack/react-query';
import { cacheGet, cacheSet } from './user-store';
import { useUser } from './user-context';
import { readUserJSON } from './user-api';

function scopedKey(sno: string, key: string): string {
  return `${sno}:${key}`;
}

async function loadCached<T>(
  key: string,
  ttlMs: number,
  network: () => Promise<T>,
  cacheFirst = false,
): Promise<T> {
  const cached = await cacheGet<T>(key).catch(() => null);
  if (cacheFirst && cached !== null) return cached;
  try {
    const value = await network();
    await cacheSet(key, value, ttlMs).catch(() => undefined);
    return value;
  } catch (error) {
    if (cached !== null) return cached;
    throw error;
  }
}

export function useUserJSON<T>(
  key: string,
  path: string,
  options: {
    enabled?: boolean;
    ttlMs: number;
    cacheFirst?: boolean;
    queryKey?: QueryKey;
  },
) {
  const { session, userFetch } = useUser();
  const enabled = Boolean(session) && options.enabled !== false;
  const cacheKey = session ? scopedKey(session.sno, key) : key;
  return useQuery<T>({
    queryKey: options.queryKey ?? ['user-data', session?.sno, key],
    queryFn: () =>
      loadCached(
        cacheKey,
        options.ttlMs,
        async () => readUserJSON<T>(await userFetch(path)),
        options.cacheFirst,
      ),
    enabled,
    staleTime: options.ttlMs,
    retry: false,
  });
}

export function useUserOptions(
  kind: 'campuses' | 'buildings' | 'rooms',
  params?: { campus?: string; building?: string },
  enabled = true,
) {
  const query = new URLSearchParams();
  if (params?.campus) query.set('campus', params.campus);
  if (params?.building) query.set('building', params.building);
  const suffix = query.toString();
  const path = `/api/user/${kind}${suffix ? `?${suffix}` : ''}`;
  return useUserJSON<Array<{ value: string; name: string }>>(
    `${kind}:${suffix}`,
    path,
    { enabled, ttlMs: 24 * 60 * 60_000, cacheFirst: true },
  );
}

export function useUserRoom(campus: string, building: string, room: string) {
  const query = new URLSearchParams({ campus, building, room });
  return useUserJSON<import('./user-api').UserRoom>(
    `room:${query.toString()}`,
    `/api/user/room?${query.toString()}`,
    { ttlMs: 30_000 },
  );
}

export function useUserOrders(status: 0 | 1) {
  return useUserJSON<{ orders: import('./user-api').RechargeOrder[] }>(
    `orders:${status}`,
    `/api/user/orders?status=${status}`,
    { ttlMs: 30_000 },
  );
}
