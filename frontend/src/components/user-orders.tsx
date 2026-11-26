import * as React from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { IconRefresh, IconX } from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { useToast } from '@/components/toast';
import { fullTs } from '@/lib/format';
import { readUserJSON, type RechargeOrder } from '@/lib/user-api';
import { useUserOrders } from '@/lib/user-cache';
import { useUser } from '@/lib/user-context';

function orderTime(order: RechargeOrder): string {
  const text = order.success_date || order.commit_date || '';
  return text ? fullTs(text) : '—';
}

function shortOrderID(orderID: string): string {
  if (orderID.length <= 12) return orderID || '—';
  return `...${orderID.slice(-10)}`;
}

export function UserOrders({
  status,
  onChanged,
}: {
  status: 0 | 1;
  onChanged?: () => void;
}) {
  const { userFetch } = useUser();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const ordersQuery = useUserOrders(status);
  const [cancelling, setCancelling] = React.useState('');
  const orders = ordersQuery.data?.orders ?? [];

  const refresh = React.useCallback(async () => {
    await ordersQuery.refetch();
    onChanged?.();
  }, [ordersQuery, onChanged]);

  async function cancel(orderID: string) {
    if (cancelling) return;
    setCancelling(orderID);
    try {
      await readUserJSON<{ ok: boolean }>(
        await userFetch('/api/user/recharge/cancel', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ order_id: orderID }),
        }),
      );
      toast('待支付订单已取消');
      await queryClient.invalidateQueries({ queryKey: ['user-data'] });
      onChanged?.();
    } catch (error) {
      toast('取消订单失败：' + (error as Error).message, true);
    } finally {
      setCancelling('');
    }
  }

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-sm font-semibold text-foreground">
            {status === 0 ? '待支付订单' : '充值记录'}
          </h3>
          <p className="text-xs text-muted-foreground">
            {orders.length === 0 ? '暂无记录' : `共 ${orders.length} 条`}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={ordersQuery.isFetching}
          onClick={() => void refresh()}
        >
          <IconRefresh className={ordersQuery.isFetching ? 'animate-spin' : ''} />
          刷新
        </Button>
      </div>

      {ordersQuery.error && (
        <p className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
          {(ordersQuery.error as Error).message}
        </p>
      )}

      {orders.length > 0 && (
        <div className="rounded-lg border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>房间</TableHead>
                <TableHead className="text-right">金额</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>订单</TableHead>
                {status === 0 && <TableHead className="text-right">操作</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {orders.map((order) => (
                <TableRow key={order.order_id}>
                  <TableCell>{orderTime(order)}</TableCell>
                  <TableCell className="max-w-64 truncate" title={order.abstracts}>
                    {order.abstracts || order.room?.room || '—'}
                  </TableCell>
                  <TableCell className="text-right font-medium">
                    ¥{Number(order.actual_amount ?? order.tran_amount).toFixed(2)}
                  </TableCell>
                  <TableCell>
                    <Badge variant={status === 0 ? 'outline' : 'default'}>
                      {status === 0 ? '待支付' : '已支付'}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {shortOrderID(order.order_id)}
                  </TableCell>
                  {status === 0 && (
                    <TableCell className="text-right">
                      <Button
                        variant="destructive"
                        size="sm"
                        disabled={Boolean(cancelling)}
                        onClick={() => void cancel(order.order_id)}
                      >
                        <IconX />
                        {cancelling === order.order_id ? '取消中...' : '取消'}
                      </Button>
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  );
}
