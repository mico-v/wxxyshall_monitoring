import * as React from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { IconAlertTriangle, IconArrowLeft, IconCircleCheck, IconCircleX } from '@tabler/icons-react';
import { BalanceBadge } from '@/components/balance-badge';
import { BalanceLegend } from '@/components/balance-bars';
import { TimeSeriesChart, type SeriesPoint } from '@/components/charts/time-series-chart';
import { ReadingsTable } from '@/components/readings-table';
import { RoomList } from '@/components/room-list';
import { useToast } from '@/components/toast';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { ToggleGroup } from '@/components/ui/toggle-group';
import { Button } from '@/components/ui/button';
import { ApiError } from '@/lib/api';
import { useApp } from '@/lib/app-context';
import { useAdmin } from '@/lib/admin';
import { usePower, useReadings, useReadingStream } from '@/lib/hooks';
import { DAY_FILTERS, groupByRoom, labelForRow, roomColorFor } from '@/lib/constants';
import { balanceDelta, balanceStatus, fmt, fullTs } from '@/lib/format';
import type { Reading, RoomGroup, RoomRef } from '@/lib/types';
import { cn } from '@/lib/utils';

const LEVEL_RANK: Record<string, number> = { '': -1, good: 0, warning: 1, serious: 2, critical: 3 };

function latest(rows: Reading[]): Reading | undefined {
  return rows[rows.length - 1];
}

function toBalancePoints(rows: Reading[]): SeriesPoint[] {
  return rows
    .filter((r) => r.surplus_charge !== null && !Number.isNaN(r.surplus_charge))
    .map((r) => ({ x: new Date(r.ts.replace(' ', 'T')).getTime(), y: r.surplus_charge as number }))
    .sort((a, b) => a.x - b.x);
}

function toPowerPoints(rows: { ts: string; power_kw: number | null }[]): SeriesPoint[] {
  return rows
    .filter((r) => r.power_kw !== null && !Number.isNaN(r.power_kw))
    .map((r) => ({ x: new Date(r.ts.replace(' ', 'T')).getTime(), y: r.power_kw as number }))
    .sort((a, b) => a.x - b.x);
}

function OverallStatusCard({ groups, loading }: { groups: RoomGroup[]; loading: boolean }) {
  if (loading || groups.length === 0) return null;
  const statuses = groups.map((g) => balanceStatus(latest(g.rows)?.surplus_charge));
  let worst = 0;
  let worstLevel = 'good';
  for (const status of statuses) {
    const rank = LEVEL_RANK[status.level] ?? -1;
    if (rank > worst) {
      worst = rank;
      worstLevel = status.level || 'good';
    }
  }
  const affected = statuses.filter((s) => LEVEL_RANK[s.level] === worst && worst > 0).length;

  const config = {
    good: {
      Icon: IconCircleCheck,
      title: `全部 ${groups.length} 间宿舍余额充足`,
      className:
        'bg-status-operational-bg border-status-operational-border text-status-operational-text',
      iconClass: 'text-status-operational',
    },
    warning: {
      Icon: IconAlertTriangle,
      title: '部分宿舍余额偏低',
      className: 'bg-status-degraded-bg border-status-degraded-border text-status-degraded-text',
      iconClass: 'text-status-degraded',
    },
    serious: {
      Icon: IconAlertTriangle,
      title: '部分宿舍余额不足',
      className: 'bg-status-serious-bg border-status-serious-border text-status-serious-text',
      iconClass: 'text-status-serious',
    },
    critical: {
      Icon: IconCircleX,
      title: '有宿舍余额临界或欠费',
      className: 'bg-status-down-bg border-status-down-border text-status-down-text',
      iconClass: 'text-status-down',
    },
  }[worstLevel as 'good' | 'warning' | 'serious' | 'critical'];

  const { Icon } = config;
  return (
    <div className={cn('flex items-center gap-3 rounded-lg border px-3 py-2.5', config.className)}>
      <Icon className={cn('size-6 shrink-0', config.iconClass)} aria-hidden="true" />
      <h2 className="text-base font-semibold text-foreground">
        {config.title}
        {affected > 0 && (
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            ({affected} / {groups.length})
          </span>
        )}
      </h2>
    </div>
  );
}

function SingleKpis({ group }: { group: RoomGroup }) {
  const rows = group.rows;
  const last = latest(rows);
  if (!last) return null;
  const delta = balanceDelta(rows);
  const status = balanceStatus(last.surplus_charge);
  const cards = [
    {
      label: '当前剩余电量',
      value: (
        <>
          {fmt(last.surplus_charge)}
          <span className="ml-1 text-sm font-medium text-muted-foreground">kWh</span>
        </>
      ),
      hero: true,
      footer: (
        <div className="mt-1 flex flex-col items-start gap-2">
          {delta.text && (
            <span
              className={cn(
                'text-xs',
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
          <BalanceBadge status={status} />
        </div>
      ),
    },
    {
      label: '电表总用电量',
      value: fmt(last.total_usage),
      footer: <span className="text-xs text-muted-foreground">kWh</span>,
    },
    {
      label: '采样点数',
      value: String(rows.length),
      footer: <span className="text-xs text-muted-foreground">当前窗口</span>,
    },
    {
      label: '最近更新',
      value: fullTs(last.ts),
      small: true,
      footer: <span className="text-xs text-muted-foreground"> </span>,
    },
  ];

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      {cards.map((card) => (
        <div key={card.label} className="rounded-lg border border-border bg-card p-4 shadow-xs">
          <div className="text-xs font-medium text-muted-foreground">{card.label}</div>
          <div
            className={cn(
              'mt-1 font-semibold tracking-tight tabular-nums',
              card.hero ? 'text-4xl' : card.small ? 'text-base font-medium' : 'text-2xl',
            )}
          >
            {card.value}
          </div>
          {card.footer}
        </div>
      ))}
    </div>
  );
}

interface DashboardProps {
  room: RoomRef | null;
  days: number;
  onDaysChange: (days: number) => void;
}

/** 宿舍页首次加载的 KPI 骨架屏：占位尺寸与真实卡片一致，避免布局跳动。 */
function KpiSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4" aria-hidden="true">
      {[0, 1, 2, 3].map((index) => (
        <div key={index} className="rounded-lg border border-border bg-card p-4 shadow-xs">
          <div className="h-3 w-20 animate-pulse rounded bg-muted" />
          <div className="mt-3 h-8 w-28 animate-pulse rounded bg-muted" />
          <div className="mt-3 h-4 w-16 animate-pulse rounded bg-muted" />
        </div>
      ))}
    </div>
  );
}

export function Dashboard({ room, days, onDaysChange }: DashboardProps) {
  const { aggregate, targets, showCollectButton } = useApp();
  const { key, setKey } = useAdmin();
  const { toast } = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const readings = useReadings({ days, room, enabled: aggregate });
  const rows = React.useMemo(() => readings.data ?? [], [readings.data]);
  const groups = React.useMemo(() => groupByRoom(rows), [rows]);
  // 主页列表按 config.json 的目标顺序展示；不在配置里的历史房间排在最后。
  const orderedGroups = React.useMemo(() => {
    const order = new Map(targets.map((t, index) => [`${t.campus}|${t.building}|${t.room}`, index]));
    return [...groups].sort((a, b) => {
      const oa = order.get(a.key);
      const ob = order.get(b.key);
      if (oa !== undefined && ob !== undefined) return oa - ob;
      if (oa !== undefined) return -1;
      if (ob !== undefined) return 1;
      return a.key.localeCompare(b.key);
    });
  }, [groups, targets]);

  // 服务端以 401 告知「主页已隐藏 / 密钥失效」时纠正本地状态：
  // 本地 config 可能来自缓存或轮询前的旧值，与服务器不一致。
  const markHomepageHidden = React.useCallback(
    (keyRejected: boolean) => {
      if (room) return;
      if (keyRejected) setKey('');
      void queryClient.invalidateQueries({ queryKey: ['config'] });
      toast(keyRejected ? '登录已失效，请重新登录' : '主页已隐藏，请登录后查看', true);
    },
    [room, setKey, queryClient, toast],
  );

  React.useEffect(() => {
    const error = readings.error as ApiError | null;
    if (!error) return;
    if (error.status === 401 && !room) {
      markHomepageHidden(!!key);
    } else if (error.status === 429 || error.status === 503) {
      toast('服务器繁忙，请稍后再试', true);
    }
  }, [readings.error, room, key, markHomepageHidden, toast]);

  useReadingStream({
    room,
    enabled: aggregate,
    key,
    onReading: () => {
      void queryClient.invalidateQueries({ queryKey: ['readings'] });
    },
    onUnauthorized: () => markHomepageHidden(!!key),
  });

  const power = usePower({ days, room }, 60, !!room && aggregate);
  const powerPoints = React.useMemo(() => toPowerPoints(power.data ?? []), [power.data]);

  const roomLabel = room
    ? groups[0]
      ? labelForRow(groups[0].rows[0]!)
      : `${room.campus}/${room.building}/${room.room}`
    : '';
  React.useEffect(() => {
    document.title = roomLabel ? `宿舍电费 · ${roomLabel}` : '宿舍电费监控';
  }, [roomLabel]);

  // 主页状态条的时间窗口：选了时间范围就固定为 [now-days, now]，
  // 「全部」则用现有数据的完整跨度。
  const window = React.useMemo(() => {
    const end = Date.now();
    if (days > 0) return { start: end - days * 86_400_000, end };
    const times = rows
      .map((r) => new Date(r.ts.replace(' ', 'T')).getTime())
      .filter((t) => !Number.isNaN(t));
    if (times.length === 0) return { start: end - 86_400_000, end };
    return { start: Math.min(...times), end: Math.max(...times) };
  }, [days, rows]);

  const singleGroup = groups[0];
  const balancePoints = React.useMemo(
    () => (singleGroup ? toBalancePoints(singleGroup.rows) : []),
    [singleGroup],
  );

  const filter = (
    <ToggleGroup
      aria-label="时间范围"
      options={DAY_FILTERS.map((f) => ({ value: f.days, label: f.label }))}
      value={days}
      onChange={onDaysChange}
    />
  );

  // 主页：一列式宿舍列表（FlareWatch 的 monitor 列表风格）。
  if (!room) {
    return (
      <div className="space-y-4">
        <OverallStatusCard groups={orderedGroups} loading={readings.isLoading} />
        <div className="flex flex-wrap items-center justify-between gap-3">
          {filter}
          <BalanceLegend />
        </div>
        <RoomList
          groups={orderedGroups}
          start={window.start}
          end={window.end}
          loading={readings.isLoading}
          days={days}
        />
      </div>
    );
  }

  // 宿舍详情：KPI、剩余电量曲线、功率曲线、读数明细。
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void navigate({ to: '/', search: { days } })}>
            <IconArrowLeft />
            全部宿舍
          </Button>
          <span className="text-sm text-muted-foreground">{roomLabel}</span>
        </div>
      </div>

      {readings.isLoading && rows.length === 0 ? (
        <KpiSkeleton />
      ) : rows.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border py-12 text-center text-sm text-muted-foreground">
          {readings.isError
            ? '读取失败，请稍后重试'
            : showCollectButton
              ? '暂无读数，可点击“立即采集”获取第一条记录'
              : '暂无读数'}
        </div>
      ) : (
        singleGroup && <SingleKpis group={singleGroup} />
      )}

      <div className="flex flex-wrap items-center gap-2">{filter}</div>

      <Card>
        <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
          <CardTitle>剩余电量曲线</CardTitle>
          <span className="text-xs text-muted-foreground">剩余电量曲线（kWh）</span>
        </CardHeader>
        <CardContent>
          <TimeSeriesChart
            data={balancePoints}
            formatValue={fmt}
            formatAxis={(v) => String(Math.round(v))}
            color={singleGroup ? roomColorFor(singleGroup.rows[0]!) : 'var(--primary)'}
            height={300}
            unit="kWh"
            emptyText="暂无有效数据"
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center justify-between gap-2">
          <CardTitle>功率曲线</CardTitle>
          <span className="text-xs text-muted-foreground">滚动平均功率（kW） · 窗口 ≥1h</span>
        </CardHeader>
        <CardContent>
          <TimeSeriesChart
            data={powerPoints}
            formatValue={(v) => v.toFixed(3)}
            color={singleGroup ? roomColorFor(singleGroup.rows[0]!) : 'var(--primary)'}
            height={200}
            unit="kW"
            emptyText={power.isLoading ? '加载中…' : '暂无有效功率数据'}
          />
        </CardContent>
      </Card>

      <ReadingsTable room={room} days={days} rows={rows} isMulti={false} colorByKey={{}} />
    </div>
  );
}
