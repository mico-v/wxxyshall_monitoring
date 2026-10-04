import * as React from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Select } from '@/components/ui/select';
import { ToggleGroup } from '@/components/ui/toggle-group';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { TABLE_PAGE_SIZES, labelForRow, type TableView } from '@/lib/constants';
import { fmt, fullTs } from '@/lib/format';
import { useDaily, useRecharges } from '@/lib/hooks';
import type { Reading, RoomRef } from '@/lib/types';
import { cn } from '@/lib/utils';

const VIEW_OPTIONS = [
  { value: 'raw', label: '原始读数' },
  { value: 'daily', label: '每天消耗' },
  { value: 'recharge', label: '充电记录' },
] as const;

function NumCell({ value, signed = false }: { value: number | null | undefined; signed?: boolean }) {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return <TableCell className="text-right text-muted-foreground">—</TableCell>;
  }
  return (
    <TableCell className={cn('text-right', value < 0 && 'font-semibold text-status-down-text')}>
      {signed && value > 0 ? '+' : ''}
      {fmt(value)}
    </TableCell>
  );
}

interface ReadingsTableProps {
  room: RoomRef | null;
  days: number;
  rows: Reading[];
  isMulti: boolean;
  colorByKey: Record<string, string>;
}

export function ReadingsTable({ room, days, rows, isMulti, colorByKey }: ReadingsTableProps) {
  const [view, setView] = React.useState<TableView>('raw');
  const [page, setPage] = React.useState(1);
  const [pageSize, setPageSize] = React.useState<number>(10);

  React.useEffect(() => {
    setPage(1);
  }, [view, pageSize, room?.campus, room?.building, room?.room, days]);

  const dailyQuery = useDaily({ days, room }, view === 'daily');
  const rechargeQuery = useRecharges({ days, room }, view === 'recharge');

  const daily = React.useMemo(
    () => (dailyQuery.data ? [...dailyQuery.data].reverse() : []),
    [dailyQuery.data],
  );
  const recharges = rechargeQuery.data ?? [];
  const raw = React.useMemo(() => [...rows].reverse(), [rows]);

  const total =
    view === 'raw'
      ? raw.length
      : view === 'daily'
        ? daily.length
        : recharges.length;
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  const currentPage = Math.min(Math.max(1, page), pageCount);
  const start = (currentPage - 1) * pageSize;

  const loading =
    (view === 'daily' && dailyQuery.isLoading) || (view === 'recharge' && rechargeQuery.isLoading);
  const errored = view === 'daily' ? dailyQuery.isError : view === 'recharge' ? rechargeQuery.isError : false;

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle>读数明细</CardTitle>
        <ToggleGroup
          aria-label="表格视图"
          options={VIEW_OPTIONS}
          value={view}
          onChange={(value) => setView(value as TableView)}
        />
        <span className="text-xs text-muted-foreground">
          {total ? `${total} ${view === 'daily' ? '天' : view === 'recharge' ? '次' : '条'} · 第 ${currentPage}/${pageCount} 页` : '—'}
        </span>
      </CardHeader>
      <CardContent>
        {loading ? (
          <p className="py-10 text-center text-xs text-muted-foreground">加载中…</p>
        ) : errored ? (
          <p className="py-10 text-center text-xs text-muted-foreground">
            {view === 'daily' ? '每天消耗加载失败' : '充电记录加载失败'}
          </p>
        ) : total === 0 ? (
          <p className="py-10 text-center text-xs text-muted-foreground">
            {view === 'daily' ? '暂无每天消耗数据' : view === 'recharge' ? '暂无充电记录' : '暂无读数'}
          </p>
        ) : (
          <Table>
            <TableHeader>
              {view === 'raw' && (
                <TableRow>
                  {isMulti && <TableHead>宿舍</TableHead>}
                  <TableHead>时间</TableHead>
                  <TableHead className="text-right">剩余电量(kWh)</TableHead>
                  <TableHead className="text-right">总用电量(kWh)</TableHead>
                  <TableHead className="text-right">消耗(kWh)</TableHead>
                  <TableHead className="text-right">平均功率(kW)</TableHead>
                </TableRow>
              )}
              {view === 'daily' && (
                <TableRow>
                  <TableHead>日期</TableHead>
                  <TableHead className="text-right">消耗电量(kWh)</TableHead>
                  <TableHead className="text-right">充电电量(kWh)</TableHead>
                  <TableHead className="text-right">平均功率(kW)</TableHead>
                  <TableHead className="text-right">日末剩余(kWh)</TableHead>
                  <TableHead className="text-right">采样数</TableHead>
                </TableRow>
              )}
              {view === 'recharge' && (
                <TableRow>
                  <TableHead>时间</TableHead>
                  <TableHead className="text-right">充值电量(kWh)</TableHead>
                  <TableHead className="text-right">充值后剩余(kWh)</TableHead>
                  <TableHead className="text-right">区间消耗(kWh)</TableHead>
                </TableRow>
              )}
            </TableHeader>
            <TableBody>
              {view === 'raw' &&
                raw.slice(start, start + pageSize).map((row, index) => (
                  <TableRow key={`${row.ts}-${row.campus}-${row.building}-${row.room}-${index}`}>
                    {isMulti && (
                      <TableCell>
                        <span
                          className="mr-2 inline-block size-2 rounded-full align-middle"
                          style={{ background: colorByKey[`${row.campus}|${row.building}|${row.room}`] }}
                        />
                        {labelForRow(row)}
                      </TableCell>
                    )}
                    <TableCell>{fullTs(row.ts)}</TableCell>
                    <NumCell value={row.surplus_charge} />
                    <NumCell value={row.total_usage} />
                    <NumCell value={row.consumption_kwh} signed />
                    <NumCell value={row.power_kw} />
                  </TableRow>
                ))}
              {view === 'daily' &&
                daily.slice(start, start + pageSize).map((row) => (
                  <TableRow key={row.day}>
                    <TableCell>{row.day}</TableCell>
                    <NumCell value={row.consumption_kwh} />
                    <NumCell value={row.recharge_kwh} signed />
                    <NumCell value={row.avg_power_kw} />
                    <NumCell value={row.surplus_end} />
                    <TableCell className="text-right">{row.samples}</TableCell>
                  </TableRow>
                ))}
              {view === 'recharge' &&
                recharges.slice(start, start + pageSize).map((row, index) => (
                  <TableRow key={`${row.ts}-${index}`}>
                    <TableCell>{fullTs(row.ts)}</TableCell>
                    <NumCell value={row.recharge_kwh} signed />
                    <NumCell value={row.surplus_after} />
                    <NumCell value={row.interval_consumption_kwh} />
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        )}

        <div className="mt-4 flex flex-wrap items-center justify-end gap-2 text-xs text-muted-foreground">
          <label className="inline-flex items-center gap-1.5">
            每页
            <Select
              className="h-7 w-auto py-0 pr-6 text-xs"
              value={String(pageSize)}
              onChange={(event) => setPageSize(Number(event.target.value) || 10)}
            >
              {TABLE_PAGE_SIZES.map((size) => (
                <option key={size} value={size}>
                  {size} 条
                </option>
              ))}
            </Select>
          </label>
          <Button
            variant="outline"
            size="sm"
            disabled={currentPage <= 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            上一页
          </Button>
          <span className="tabular-nums">
            {total ? `${currentPage} / ${pageCount}` : '0 / 0'}
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={currentPage >= pageCount || total === 0}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
