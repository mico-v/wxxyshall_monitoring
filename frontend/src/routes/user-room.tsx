import * as React from 'react';
import { createRoute, Link, useNavigate } from '@tanstack/react-router';
import { useQueryClient } from '@tanstack/react-query';
import {
  IconArrowLeft,
  IconBackspace,
  IconCircleCheck,
  IconClock,
  IconCreditCard,
  IconKey,
  IconRefresh,
  IconWallet,
  IconX,
} from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { UserOrders } from '@/components/user-orders';
import { useToast } from '@/components/toast';
import { ApiError } from '@/lib/api';
import { PAGE_CONTAINER_CLASSES } from '@/lib/constants';
import { fmt } from '@/lib/format';
import {
  readUserJSON,
  type CreateOrderResponse,
  type PaymentMethod,
  type PreparePaymentResponse,
  type RechargeOrder,
} from '@/lib/user-api';
import { useUserRoom } from '@/lib/user-cache';
import { useUser } from '@/lib/user-context';
import { cn } from '@/lib/utils';
import { rootRoute } from './__root';

export const userRoomRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/user/room/$campus/$building/$room',
  component: UserRoomPage,
});

interface PendingOrder {
  order: RechargeOrder;
  method: PaymentMethod;
  amount: number;
}

interface PreparedPayment {
  orderID: string;
  amount: number;
  method: PaymentMethod;
  uuid: string;
  keys: number[];
}

interface SubmittedPayment {
  orderID: string;
  amount: number;
  state: 'submitting' | 'submitted' | 'unknown';
}

function UserRoomPage() {
  const { session } = useUser();
  if (!session) {
    return (
      <div className={PAGE_CONTAINER_CLASSES}>
        <Card>
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>登录学校账号后才能查询房间和发起充值。</CardDescription>
          </CardHeader>
          <CardContent>
            <Link to="/user" className="text-sm font-medium text-primary hover:underline">
              前往登录
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  }
  return <UserRoomContent />;
}

function UserRoomContent() {
  const params = userRoomRoute.useParams();
  const navigate = useNavigate();
  const { userFetch } = useUser();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const roomQuery = useUserRoom(params.campus, params.building, params.room);
  const [amountText, setAmountText] = React.useState('10');
  const [pending, setPending] = React.useState<PendingOrder | null>(null);
  const [prepared, setPrepared] = React.useState<PreparedPayment | null>(null);
  const [submittedPayment, setSubmittedPayment] = React.useState<SubmittedPayment | null>(null);
  const [creating, setCreating] = React.useState(false);
  const [preparing, setPreparing] = React.useState(false);
  const [paying, setPaying] = React.useState(false);
  const [cancelling, setCancelling] = React.useState(false);
  const [creationUncertain, setCreationUncertain] = React.useState(false);
  const [message, setMessage] = React.useState('');
  const [error, setError] = React.useState('');
  const digitsRef = React.useRef('');
  const createLockRef = React.useRef(false);
  const [digitCount, setDigitCount] = React.useState(0);
  const postPaymentTimers = React.useRef<number[]>([]);

  const room = roomQuery.data;
  const amount = Number(amountText);
  const amountValid =
    Number.isFinite(amount) &&
    amount >= (room?.amount_limits.min ?? 1) &&
    amount <= (room?.amount_limits.max ?? 500);

  React.useEffect(() => {
    return () => {
      for (const timer of postPaymentTimers.current) window.clearTimeout(timer);
    };
  }, []);

  function clearDigits() {
    digitsRef.current = '';
    setDigitCount(0);
  }

  function invalidateUserData() {
    return queryClient.invalidateQueries({ queryKey: ['user-data'] });
  }

  function schedulePostPaymentRefresh() {
    for (const timer of postPaymentTimers.current) window.clearTimeout(timer);
    postPaymentTimers.current = [4, 8, 15, 30].map((seconds) =>
      window.setTimeout(() => {
        void invalidateUserData();
      }, seconds * 1000),
    );
  }

  async function createOrder() {
    if (!amountValid || creating || pending || submittedPayment || createLockRef.current) return;
    createLockRef.current = true;
    setCreating(true);
    setCreationUncertain(false);
    setError('');
    setMessage('');
    try {
      const result = await readUserJSON<CreateOrderResponse>(
        await userFetch('/api/user/recharge/order', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            campus: params.campus,
            building: params.building,
            room: params.room,
            amount,
          }),
        }),
      );
      setPending({
        order: result.order,
        method: result.payment_method,
        amount,
      });
      setPrepared(null);
      clearDigits();
      setMessage('充值订单已创建，请核对房间、金额和支付方式后继续。');
      await invalidateUserData();
    } catch (createError) {
      setError((createError as Error).message);
      if (!(createError instanceof ApiError)) setCreationUncertain(true);
    } finally {
      setCreating(false);
      createLockRef.current = false;
    }
  }

  async function preparePayment() {
    if (!pending || preparing) return;
    setPreparing(true);
    setError('');
    setMessage('');
    try {
      const result = await readUserJSON<PreparePaymentResponse>(
        await userFetch('/api/user/recharge/prepare', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ order_id: pending.order.order_id }),
        }),
      );
      const keys = result.password_map.keys;
      if (
        keys.length !== 10 ||
        new Set(keys).size !== 10 ||
        keys.some((key) => !Number.isInteger(key) || key < 0 || key > 9)
      ) {
        throw new Error('学校返回的支付安全键盘无效，请重新发起充值');
      }
      clearDigits();
      setPrepared({
        orderID: pending.order.order_id,
        amount: pending.amount,
        method: result.payment_method,
        uuid: result.password_map.uuid,
        keys,
      });
      setMessage('请输入六位校园卡密码。键盘位置只在本次支付中使用。');
    } catch (prepareError) {
      setError((prepareError as Error).message);
    } finally {
      setPreparing(false);
    }
  }

  async function cancelOrder() {
    if (!pending || cancelling || paying) return;
    setCancelling(true);
    setError('');
    try {
      await readUserJSON<{ ok: boolean }>(
        await userFetch('/api/user/recharge/cancel', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ order_id: pending.order.order_id }),
        }),
      );
      setPending(null);
      setPrepared(null);
      clearDigits();
      setMessage('待支付订单已取消。');
      await invalidateUserData();
    } catch (cancelError) {
      setError((cancelError as Error).message);
    } finally {
      setCancelling(false);
    }
  }

  function pressDigit(digit: number) {
    if (!prepared || digitsRef.current.length >= 6) return;
    digitsRef.current += String(digit);
    setDigitCount(digitsRef.current.length);
  }

  async function submitPayment() {
    if (!prepared || paying || digitsRef.current.length !== 6) return;
    const passwordDigits = digitsRef.current;
    const indexes: string[] = [];
    for (const digitChar of passwordDigits) {
      const digit = Number(digitChar);
      const index = prepared.keys.indexOf(digit);
      if (index < 0) {
        setError('安全键盘映射无效，请重新获取键盘。');
        clearDigits();
        return;
      }
      indexes.push(String(index));
    }

    const payment = prepared;
    clearDigits();
    setPrepared(null);
    setPending(null);
    setSubmittedPayment({
      orderID: payment.orderID,
      amount: payment.amount,
      state: 'submitting',
    });
    setPaying(true);
    setError('');
    setMessage('');
    try {
      await readUserJSON<{ ok: boolean; poll_hint?: string }>(
        await userFetch('/api/user/recharge/pay', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            order_id: payment.orderID,
            uuid: payment.uuid,
            password_indexes: indexes.join(''),
          }),
        }),
      );
      setSubmittedPayment({
        orderID: payment.orderID,
        amount: payment.amount,
        state: 'submitted',
      });
      toast('支付请求已提交');
      await invalidateUserData();
      schedulePostPaymentRefresh();
    } catch (payError) {
      setSubmittedPayment({
        orderID: payment.orderID,
        amount: payment.amount,
        state: 'unknown',
      });
      setError(
        `${(payError as Error).message}。支付结果可能尚未确认，请先刷新订单状态，不要直接重复提交。`,
      );
      await invalidateUserData();
      schedulePostPaymentRefresh();
    } finally {
      setPaying(false);
    }
  }

  if (roomQuery.isLoading) {
    return (
      <div className={`${PAGE_CONTAINER_CLASSES} flex min-h-72 items-center justify-center`}>
        <span className="text-sm text-muted-foreground">正在查询房间...</span>
      </div>
    );
  }

  if (roomQuery.error || !room) {
    return (
      <div className={PAGE_CONTAINER_CLASSES}>
        <Card>
          <CardHeader>
            <CardTitle>房间查询失败</CardTitle>
            <CardDescription>{(roomQuery.error as Error)?.message || '学校没有返回房间数据'}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => void roomQuery.refetch()}>
              <IconRefresh />
              重试
            </Button>
            <Button variant="ghost" onClick={() => void navigate({ to: '/user' })}>
              <IconArrowLeft />
              返回
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className={`${PAGE_CONTAINER_CLASSES} space-y-5`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <Link
            to="/user"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <IconArrowLeft className="size-3.5" />
            返回账号入口
          </Link>
          <h1 className="mt-2 text-2xl font-semibold">
            {[room.campus_name, room.building_name, room.room_name].filter(Boolean).join(' / ') ||
              params.room}
          </h1>
          <p className="mt-1 text-xs text-muted-foreground">
            {params.campus} / {params.building} / {params.room}
          </p>
        </div>
        <Badge variant="outline">校园卡充值</Badge>
      </div>

      <Card>
        <CardHeader className="flex-row flex-wrap items-start justify-between gap-2">
          <div>
            <CardTitle>当前房间余额</CardTitle>
            <CardDescription>学校计量接口返回的实时快照</CardDescription>
          </div>
          <Button variant="outline" size="sm" onClick={() => void roomQuery.refetch()}>
            <IconRefresh className={roomQuery.isFetching ? 'animate-spin' : ''} />
            刷新
          </Button>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-baseline gap-2">
            <span className="text-3xl font-semibold tabular-nums">
              {fmt(room.reading.surplus_charge)}
            </span>
            <span className="text-sm text-muted-foreground">剩余电量</span>
          </div>
          {Object.keys(room.reading.show).length > 0 && (
            <dl className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {Object.entries(room.reading.show).map(([label, value]) => (
                <div key={label} className="rounded-md border border-border bg-muted/30 px-3 py-2">
                  <dt className="text-xs text-muted-foreground">{label}</dt>
                  <dd className="mt-1 text-sm font-medium tabular-nums">{value}</dd>
                </div>
              ))}
            </dl>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <IconWallet className="size-5 text-primary" />
            为房间充值
          </CardTitle>
          <CardDescription>
            创建订单不会立即扣款。核对订单后输入校园卡查询密码才会提交支付。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {!pending && !prepared && !submittedPayment && (
            <>
              <div className="space-y-2">
                <Label>充值金额</Label>
                <div className="flex flex-wrap gap-2">
                  {room.amount_options.map((option) => (
                    <Button
                      key={option}
                      variant={amount === option ? 'default' : 'outline'}
                      onClick={() => setAmountText(String(option))}
                    >
                      ¥{option}
                    </Button>
                  ))}
                  <div className="flex min-w-36 flex-1 items-center gap-2 sm:max-w-52">
                    <span className="text-sm text-muted-foreground">¥</span>
                    <Input
                      type="number"
                      min={room.amount_limits.min}
                      max={room.amount_limits.max}
                      step="1"
                      inputMode="decimal"
                      value={amountText}
                      onChange={(event) => setAmountText(event.target.value)}
                      aria-label="自定义充值金额"
                    />
                  </div>
                </div>
                <p className="text-xs text-muted-foreground">
                  单笔限额 ¥{room.amount_limits.min} - ¥{room.amount_limits.max}
                </p>
              </div>
              <Button
                disabled={!amountValid || creating || creationUncertain}
                onClick={() => void createOrder()}
              >
                <IconCreditCard />
                {creating ? '正在创建订单...' : `创建 ¥${amountValid ? amount.toFixed(2) : '—'} 待支付订单`}
              </Button>
              {creationUncertain && (
                <div className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2">
                  <p className="min-w-0 flex-1 text-xs text-destructive">
                    订单创建结果未知，请先核对待支付记录，避免重复创建。
                  </p>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setCreationUncertain(false);
                      void invalidateUserData();
                    }}
                  >
                    已核对待支付记录
                  </Button>
                </div>
              )}
            </>
          )}

          {pending && !prepared && (
            <div className="space-y-4">
              <div className="rounded-lg border border-border bg-muted/30 p-4">
                <p className="text-xs text-muted-foreground">待支付订单</p>
                <p className="mt-1 text-2xl font-semibold">¥{pending.amount.toFixed(2)}</p>
                <dl className="mt-3 grid gap-2 text-sm sm:grid-cols-2">
                  <div className="flex justify-between gap-3 sm:block">
                    <dt className="text-muted-foreground">房间</dt>
                    <dd className="mt-0.5">{pending.order.abstracts || params.room}</dd>
                  </div>
                  <div className="flex justify-between gap-3 sm:block">
                    <dt className="text-muted-foreground">支付方式</dt>
                    <dd className="mt-0.5">{pending.method.name} / {pending.method.code}</dd>
                  </div>
                  <div className="flex justify-between gap-3 sm:block">
                    <dt className="text-muted-foreground">订单号</dt>
                    <dd className="mt-0.5 font-mono text-xs">{pending.order.order_id}</dd>
                  </div>
                </dl>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button disabled={preparing || cancelling} onClick={() => void preparePayment()}>
                  <IconKey />
                  {preparing ? '正在获取安全键盘...' : '继续支付'}
                </Button>
                <Button
                  variant="destructive"
                  disabled={preparing || cancelling}
                  onClick={() => void cancelOrder()}
                >
                  <IconX />
                  {cancelling ? '正在取消...' : '取消订单'}
                </Button>
              </div>
            </div>
          )}

          {prepared && (
            <div className="space-y-4">
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-primary/30 bg-primary/5 px-3 py-2">
                <div>
                  <p className="text-xs text-muted-foreground">确认支付</p>
                  <p className="text-lg font-semibold">¥{prepared.amount.toFixed(2)}</p>
                </div>
                <Badge>{prepared.method.name}</Badge>
              </div>

              <div>
                <div className="mb-2 flex items-center justify-between">
                  <Label>六位校园卡密码</Label>
                  <div className="flex items-center gap-1">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label="退格"
                      disabled={digitCount === 0 || paying}
                      onClick={() => {
                        digitsRef.current = digitsRef.current.slice(0, -1);
                        setDigitCount(digitsRef.current.length);
                      }}
                    >
                      <IconBackspace />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label="清空密码"
                      disabled={digitCount === 0 || paying}
                      onClick={clearDigits}
                    >
                      <IconX />
                    </Button>
                  </div>
                </div>
                <div className="mb-3 flex gap-2">
                  {Array.from({ length: 6 }, (_, index) => (
                    <span
                      key={index}
                      className="flex size-10 items-center justify-center rounded-md border border-border bg-muted text-lg font-semibold"
                    >
                      {index < digitCount ? '•' : ''}
                    </span>
                  ))}
                </div>
                <div className="grid max-w-md grid-cols-5 gap-2">
                  {prepared.keys.map((digit, index) => (
                    <Button
                      key={`${digit}-${index}`}
                      variant="outline"
                      className="h-12 text-lg font-semibold"
                      disabled={digitCount >= 6 || paying}
                      aria-label={`安全键盘第 ${index + 1} 个数字`}
                      onClick={() => pressDigit(digit)}
                    >
                      {digit}
                    </Button>
                  ))}
                </div>
                <p className="mt-2 text-xs text-muted-foreground">
                  密码只保留在当前页面内存；提交给本站的是随机键盘位置索引。
                </p>
              </div>

              <div className="flex flex-wrap gap-2">
                <Button
                  disabled={digitCount !== 6 || paying}
                  onClick={() => void submitPayment()}
                >
                  <IconCircleCheck />
                  {paying ? '正在提交...' : `确认支付 ¥${prepared.amount.toFixed(2)}`}
                </Button>
                <Button
                  variant="ghost"
                  disabled={paying}
                  onClick={() => {
                    clearDigits();
                    setPrepared(null);
                    setMessage('已取消本次密码输入，待支付订单仍然保留。');
                  }}
                >
                  返回订单
                </Button>
              </div>
            </div>
          )}

          {submittedPayment && (
            <div
              className={cn(
                'rounded-lg border p-4',
                submittedPayment.state === 'unknown'
                  ? 'border-status-degraded-border bg-status-degraded-bg'
                  : 'border-status-operational-border bg-status-operational-bg',
              )}
            >
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <p
                    className={cn(
                      'flex items-center gap-2 text-sm font-semibold',
                      submittedPayment.state === 'unknown'
                        ? 'text-status-degraded-text'
                        : 'text-status-operational-text',
                    )}
                  >
                    {submittedPayment.state === 'submitted' ? (
                      <IconCircleCheck className="size-4" />
                    ) : (
                      <IconClock className="size-4" />
                    )}
                    {submittedPayment.state === 'submitting'
                      ? '正在提交支付'
                      : submittedPayment.state === 'unknown'
                        ? '支付结果待确认'
                        : '支付请求已提交'}
                  </p>
                  <p className="mt-1 text-sm">
                    订单 {submittedPayment.orderID}，金额 ¥{submittedPayment.amount.toFixed(2)}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {submittedPayment.state === 'submitting'
                      ? '请勿关闭页面或重复操作。'
                      : '学校订单状态和电表到账可能延迟。请查询订单记录确认结果，不要重复支付同一订单。'}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={paying}
                  onClick={() => void invalidateUserData()}
                >
                  <IconRefresh />
                  刷新状态
                </Button>
              </div>
            </div>
          )}

          {error && (
            <p className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
              {error}
            </p>
          )}
          {message && <p className="text-xs text-muted-foreground">{message}</p>}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>订单记录</CardTitle>
          <CardDescription>支付状态和电表到账可能分阶段更新。</CardDescription>
        </CardHeader>
        <CardContent>
          <UserOrders status={1} />
        </CardContent>
      </Card>
    </div>
  );
}
