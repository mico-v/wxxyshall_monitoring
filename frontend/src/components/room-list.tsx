import * as React from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { BalanceBadge, BalanceIcon } from '@/components/balance-badge';
import { BalanceBars, buildBalanceBuckets } from '@/components/balance-bars';
import { balanceDelta, balanceStatus, fmt } from '@/lib/format';
import { prefetchRoom } from '@/lib/hooks';
import { useAdmin } from '@/lib/admin';
import { useApp } from '@/lib/app-context';
import type { RoomGroup } from '@/lib/types';
import { cn } from '@/lib/utils';

function latest(group: RoomGroup) {
  return group.rows[group.rows.length - 1];
}

function RoomRow({
  group,
  start,
  end,
  days,
}: {
  group: RoomGroup;
  start: number;
  end: number;
  days: number;
}) {
  const last = latest(group);
  const first = group.rows[0];
  const status = balanceStatus(last?.surplus_charge);
  const delta = balanceDelta(group.rows);
  const buckets = React.useMemo(
    () => buildBalanceBuckets(group.rows, start, end),
    [group.rows, start, end],
  );

  // 悬停/聚焦/触摸即预取，点击时数据已就绪。
  const queryClient = useQueryClient();
  const { key } = useAdmin();
  const prefetched = React.useRef(false);
  const handlePrefetch = React.useCallback(() => {
    if (prefetched.current || !first) return;
    prefetched.current = true;
    prefetchRoom(
      queryClient,
      { campus: first.campus, building: first.building, room: first.room },
      days,
      key,
    );
  }, [queryClient, first, days, key]);

  if (!first) return null;

  return (
    <Link
      to="/room/$campus/$building/$room"
      params={{ campus: first.campus, building: first.building, room: first.room }}
      search={{ days }}
      onMouseEnter={handlePrefetch}
      onFocus={handlePrefetch}
      onTouchStart={handlePrefetch}
      className="group block px-4 py-3 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
      aria-label={`${group.label}，${status.text}，剩余 ${fmt(last?.surplus_charge)} kWh，点击查看明细`}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <BalanceIcon level={status.level} />
          <span className="truncate text-sm font-medium text-foreground">{group.label}</span>
          <BalanceBadge status={status} className="hidden sm:inline-flex" />
          {delta.text && (
            <span
              className={cn(
                'hidden text-xs tabular-nums sm:inline',
                delta.tone === 'up'
                  ? 'text-status-operational-text'
                  : delta.tone === 'down'
                    ? 'text-status-down-text'
                    : 'text-muted-foreground',
              )}
            >
              {delta.text}
            </span>
          )}
        </div>
        <div className="flex shrink-0 items-baseline gap-1 tabular-nums">
          <span className="text-lg font-semibold text-foreground">
            {fmt(last?.surplus_charge)}
          </span>
          <span className="text-xs text-muted-foreground">kWh</span>
        </div>
      </div>
      <BalanceBars buckets={buckets} className="mt-2.5" />
    </Link>
  );
}

interface RoomListProps {
  groups: RoomGroup[];
  start: number;
  end: number;
  loading: boolean;
  /** 当前时间范围，进入详情时透传，避免钻取后筛选被重置。 */
  days: number;
}

/** 主页：一列式宿舍列表，每行下方是颜色映射电量的状态条。 */
export function RoomList({ groups, start, end, loading, days }: RoomListProps) {
  const { showCollectButton } = useApp();
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card shadow-xs">
      <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
        <h2 className="text-base font-semibold text-foreground">宿舍</h2>
        <span className="text-xs text-muted-foreground">
          {loading && groups.length === 0 ? '加载中…' : `${groups.length} 间 · 点击查看明细`}
        </span>
      </div>
      {groups.length === 0 ? (
        <p className="px-4 py-12 text-center text-sm text-muted-foreground">
          {loading ? '加载中…' : showCollectButton ? '暂无读数，可点击“立即采集”获取第一条记录' : '暂无读数'}
        </p>
      ) : (
        <div className="divide-y divide-border">
          {groups.map((group) => (
            <RoomRow key={group.key} group={group} start={start} end={end} days={days} />
          ))}
        </div>
      )}
    </div>
  );
}
