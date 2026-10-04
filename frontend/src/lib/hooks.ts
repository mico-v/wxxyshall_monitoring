import * as React from 'react';
import { useQuery, type QueryClient } from '@tanstack/react-query';
import { getConfig, getDaily, getPower, getReadings, getRecharges, type RoomQuery } from './api';
import { useAdmin } from './admin';
import { roomKey } from './constants';
import type { Reading, RoomRef } from './types';

export function usePublicConfig(room: RoomRef | null) {
  const { key, ready } = useAdmin();
  const roomId = room ? roomKey(room) : '';
  return useQuery({
    queryKey: ['config', roomId, key],
    queryFn: ({ signal }) => getConfig({ room, key }, signal),
    enabled: ready,
    staleTime: 30_000,
    refetchInterval: 30_000,
  });
}

export function useReadings(opts: { days: number; room: RoomRef | null; enabled: boolean }) {
  const { key } = useAdmin();
  const roomId = opts.room ? roomKey(opts.room) : '';
  const aggregate = !opts.room;
  // 聚合读数可能受主页可见性限制，缓存键含密钥；单宿舍公开数据无需密钥。
  const authKey = aggregate ? key : '';
  return useQuery<Reading[]>({
    queryKey: ['readings', opts.days, roomId, authKey],
    queryFn: ({ signal }) => getReadings({ days: opts.days, room: opts.room }, authKey, signal),
    enabled: opts.enabled,
    staleTime: 30_000,
    retry: (count, error) => {
      const status = (error as { status?: number }).status;
      if (status === 401 || status === 429 || status === 503) return false;
      return count < 2;
    },
  });
}

function useRoomSeries<T>(
  key: string,
  query: RoomQuery,
  fetcher: (q: RoomQuery, signal?: AbortSignal) => Promise<T>,
  enabled: boolean,
) {
  const roomId = query.room ? roomKey(query.room) : '';
  return useQuery<T>({
    queryKey: [key, query.days ?? 0, roomId],
    queryFn: ({ signal }) => fetcher(query, signal),
    enabled,
    staleTime: 60_000,
  });
}

export function useDaily(query: RoomQuery, enabled: boolean) {
  return useRoomSeries('daily', query, getDaily, enabled);
}

export function useRecharges(query: RoomQuery, enabled: boolean) {
  return useRoomSeries('recharges', query, getRecharges, enabled);
}

export function usePower(query: RoomQuery, bucketMinutes: number, enabled: boolean) {
  const roomId = query.room ? roomKey(query.room) : '';
  return useQuery({
    queryKey: ['power', query.days ?? 0, roomId, bucketMinutes],
    queryFn: ({ signal }) => getPower(query, bucketMinutes, signal),
    enabled,
    staleTime: 60_000,
  });
}

/**
 * 主页宿舍行悬停/聚焦时预取该宿舍的数据，点击进入时无需等首屏请求，
 * 避免“第一次点击闪一下、之后有缓存才顺”的观感。查询键必须与对应 hook 完全一致。
 */
export function prefetchRoom(
  queryClient: QueryClient,
  room: RoomRef,
  days: number,
  key: string,
): void {
  const roomId = roomKey(room);
  void queryClient.prefetchQuery({
    queryKey: ['readings', days, roomId, ''],
    queryFn: ({ signal }) => getReadings({ days, room }, '', signal),
    staleTime: 30_000,
  });
  void queryClient.prefetchQuery({
    queryKey: ['config', roomId, key],
    queryFn: ({ signal }) => getConfig({ room, key }, signal),
    staleTime: 30_000,
  });
  void queryClient.prefetchQuery({
    queryKey: ['power', days, roomId, 60],
    queryFn: ({ signal }) => getPower({ days, room }, 60, signal),
    staleTime: 60_000,
  });
}

/** SSE 实时读数：聚合或单宿舍，断线时按 60s 轮询兜底。 */
export function useReadingStream(opts: {
  room: RoomRef | null;
  enabled: boolean;
  key: string;
  onReading: (reading: unknown) => void;
  /** 连接被 401 拒绝（如主页刚被隐藏 / 密钥失效）时回调，用于纠正本地视图。 */
  onUnauthorized?: () => void;
}) {
  const { room, enabled, key, onReading, onUnauthorized } = opts;
  const onReadingRef = React.useRef(onReading);
  onReadingRef.current = onReading;
  const onUnauthorizedRef = React.useRef(onUnauthorized);
  onUnauthorizedRef.current = onUnauthorized;

  React.useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    let connected = false;
    let pollTimer: number | undefined;

    const startPolling = () => {
      if (pollTimer !== undefined) return;
      pollTimer = window.setInterval(() => {
        if (!connected) onReadingRef.current({ type: 'poll' });
      }, 60_000);
    };

    const params = new URLSearchParams();
    if (room) {
      params.set('campus', room.campus);
      params.set('building', room.building);
      params.set('room', room.room);
    }
    const url = '/api/events' + (params.toString() ? `?${params.toString()}` : '');
    const headers = new Headers();
    if (!room && key) headers.set('Authorization', `Bearer ${key}`);

    (async () => {
      try {
        const response = await fetch(url, {
          headers,
          cache: 'no-store',
          signal: controller.signal,
        });
        if (!response.ok || !response.body) {
          if (response.status === 401) onUnauthorizedRef.current?.();
          throw new Error(`HTTP ${response.status}`);
        }
        connected = true;
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          let end: number;
          while ((end = buffer.indexOf('\n\n')) >= 0) {
            handleBlock(buffer.slice(0, end), onReadingRef.current);
            buffer = buffer.slice(end + 2);
          }
        }
      } catch (error) {
        if (!controller.signal.aborted) console.warn('SSE 连接中断:', error);
      }
      connected = false;
      if (!controller.signal.aborted) startPolling();
    })();

    startPolling();

    return () => {
      controller.abort();
      if (pollTimer !== undefined) window.clearInterval(pollTimer);
    };
  }, [room?.campus, room?.building, room?.room, enabled, key]);
}

function handleBlock(block: string, onReading: (reading: unknown) => void) {
  let event = 'message';
  const data: string[] = [];
  for (const line of block.split('\n')) {
    if (!line || line.startsWith(':')) continue;
    const colon = line.indexOf(':');
    if (colon < 0) continue;
    const field = line.slice(0, colon);
    const value = line.slice(colon + 1).replace(/^ /, '');
    if (field === 'event') event = value;
    else if (field === 'data') data.push(value);
  }
  if (!data.length) return;
  if (event !== 'reading') return;
  try {
    onReading(JSON.parse(data.join('\n')));
  } catch (error) {
    console.warn('SSE reading 解析失败:', error);
  }
}
