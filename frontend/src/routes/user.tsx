import * as React from 'react';
import { createRoute, Link, useNavigate } from '@tanstack/react-router';
import {
  IconAlertTriangle,
  IconCards,
  IconClock,
  IconLogin,
  IconReceipt,
  IconServer,
  IconUserCircle,
} from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { UserOrders } from '@/components/user-orders';
import { UserRoomPicker } from '@/components/user-room-picker';
import { useToast } from '@/components/toast';
import { ApiError } from '@/lib/api';
import { ADMIN_KEY_STORE, PAGE_CONTAINER_CLASSES } from '@/lib/constants';
import { useUser } from '@/lib/user-context';
import { rootRoute } from './__root';

export const userRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/user',
  component: UserPage,
});

function UserPage() {
  const { ready, session } = useUser();
  if (!ready) {
    return (
      <div className={`${PAGE_CONTAINER_CLASSES} flex min-h-72 items-center justify-center`}>
        <span className="text-sm text-muted-foreground">正在恢复本地登录状态...</span>
      </div>
    );
  }
  return (
    <div className={PAGE_CONTAINER_CLASSES}>
      {session ? <UserOverview /> : <UserLogin />}
    </div>
  );
}

function UserLogin() {
  const { login } = useUser();
  const [username, setUsername] = React.useState('');
  const passwordRef = React.useRef<HTMLInputElement>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const [confirmMonitor, setConfirmMonitor] = React.useState(false);
  const [error, setError] = React.useState('');

  async function submit(event?: React.FormEvent<HTMLFormElement>, confirm = false) {
    event?.preventDefault();
    const password = passwordRef.current?.value ?? '';
    setSubmitting(true);
    setConfirmMonitor(false);
    setError('');
    try {
      await login(username, password, confirm);
      if (passwordRef.current) passwordRef.current.value = '';
    } catch (loginError) {
      if (
        loginError instanceof ApiError &&
        loginError.payload?.requires_monitor_confirmation === true
      ) {
        setConfirmMonitor(true);
      } else {
        setError((loginError as Error).message);
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_360px] lg:items-start">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <IconUserCircle className="size-5 text-primary" />
            学校账号登录
          </CardTitle>
          <CardDescription>
            登录后可查询宿舍电费、充值记录，并使用校园卡为房间充值。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={(event) => void submit(event)}>
            <div className="space-y-1.5">
              <Label htmlFor="portal-username">学校账号</Label>
              <Input
                id="portal-username"
                value={username}
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
                maxLength={128}
                placeholder="学号"
                onChange={(event) => setUsername(event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="portal-password">学校密码</Label>
              <Input
                id="portal-password"
                ref={passwordRef}
                type="password"
                autoComplete="off"
                maxLength={256}
                spellCheck={false}
                placeholder="输入密码"
              />
            </div>
            <p className="rounded-md border border-status-degraded-border bg-status-degraded-bg px-3 py-2 text-xs text-status-degraded-text">
              学校对同一账号只保留一个设备会话。若该账号也用于服务器监控，浏览器登录前会要求二次确认，登录后可把新 token 同步给监控服务。
            </p>
            {confirmMonitor && (
              <div className="rounded-md border border-status-degraded-border bg-status-degraded-bg p-3">
                <p className="flex items-center gap-2 text-sm font-medium text-status-degraded-text">
                  <IconAlertTriangle className="size-4" />
                  该账号正在用于服务器监控
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  继续登录会使服务器当前学校会话失效。登录完成后可在总览页把新 token
                  同步回监控服务。
                </p>
                <div className="mt-3 flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    disabled={submitting}
                    onClick={() => void submit(undefined, true)}
                  >
                    仍然登录
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setConfirmMonitor(false);
                      setError('');
                    }}
                  >
                    取消
                  </Button>
                </div>
              </div>
            )}
            {error && <p className="text-xs text-destructive">{error}</p>}
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="max-w-md text-xs text-muted-foreground">
                密码只在当前页面内存中完成安全键盘映射，不会提交明文，也不会写入本地存储。
              </p>
              <Button
                type="submit"
                disabled={submitting || confirmMonitor || !username.trim()}
              >
                <IconLogin />
                {submitting ? '正在登录...' : '登录'}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <div className="grid gap-3">
        <InfoRow icon={<IconCards />} title="本地登录状态" text="学校 token 使用浏览器 AES-GCM 加密保存。" />
        <InfoRow icon={<IconReceipt />} title="充值记录" text="待支付与已支付订单均按当前学校账号查询。" />
        <InfoRow icon={<IconClock />} title="支付结果" text="扣款成功后订单状态可能异步更新，页面会继续查询。" />
      </div>
    </div>
  );
}

function InfoRow({ icon, title, text }: { icon: React.ReactNode; title: string; text: string }) {
  return (
    <div className="flex gap-3 rounded-lg border border-border bg-muted/30 p-3">
      <span className="mt-0.5 text-primary [&>svg]:size-5">{icon}</span>
      <div>
        <p className="text-sm font-medium">{title}</p>
        <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{text}</p>
      </div>
    </div>
  );
}

function UserOverview() {
  const { session } = useUser();
  const navigate = useNavigate();
  const [orderStatus, setOrderStatus] = React.useState<0 | 1>(1);

  const expiresInDays = React.useMemo(() => {
    if (!session || session.expiresIn <= 0) return null;
    const secondsLeft = session.loginTime + session.expiresIn - Date.now() / 1000;
    return Math.max(0, Math.floor(secondsLeft / 86400));
  }, [session]);

  if (!session) return null;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-xs font-medium text-primary">校园卡服务</p>
          <h1 className="mt-1 text-2xl font-semibold">查询与充值</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            当前账号 {session.sno}
            {expiresInDays !== null ? `，登录状态约剩 ${expiresInDays} 天` : ''}
          </p>
        </div>
        <Link to="/" className="text-sm text-muted-foreground hover:text-foreground">
          返回电费监控
        </Link>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>选择宿舍</CardTitle>
          <CardDescription>依次选择校区、楼栋和房间。</CardDescription>
        </CardHeader>
        <CardContent>
          <UserRoomPicker
            onSelect={(room) =>
              void navigate({
                to: '/user/room/$campus/$building/$room',
                params: room,
              })
            }
          />
        </CardContent>
      </Card>

      <MonitorSyncPanel />

      <Card>
        <CardHeader className="gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <CardTitle>学校订单</CardTitle>
              <CardDescription>订单号只显示尾部，用于区分记录。</CardDescription>
            </div>
            <div className="flex rounded-md border border-border bg-muted p-0.5">
              <Button
                size="sm"
                variant={orderStatus === 1 ? 'default' : 'ghost'}
                onClick={() => setOrderStatus(1)}
              >
                充值记录
              </Button>
              <Button
                size="sm"
                variant={orderStatus === 0 ? 'default' : 'ghost'}
                onClick={() => setOrderStatus(0)}
              >
                待支付
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <UserOrders status={orderStatus} />
        </CardContent>
      </Card>
    </div>
  );
}

function MonitorSyncPanel() {
  const { session, syncToServer } = useUser();
  const { toast } = useToast();
  const [open, setOpen] = React.useState(false);
  const [syncing, setSyncing] = React.useState(false);
  const [synced, setSynced] = React.useState(false);
  const [error, setError] = React.useState('');
  const [adminKey, setAdminKey] = React.useState(() => {
    try {
      return localStorage.getItem(ADMIN_KEY_STORE) || '';
    } catch {
      return '';
    }
  });

  if (!session) return null;

  async function sync() {
    setSyncing(true);
    setError('');
    try {
      await syncToServer(adminKey);
      try {
        localStorage.setItem(ADMIN_KEY_STORE, adminKey.trim());
      } catch {
        /* ignore localStorage failures */
      }
      setSynced(true);
      setOpen(false);
      toast('当前学校 token 已同步到监控服务');
    } catch (syncError) {
      setError((syncError as Error).message);
    } finally {
      setSyncing(false);
    }
  }

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-start justify-between gap-3">
        <div>
          <CardTitle className="flex items-center gap-2">
            <IconServer className="size-5 text-primary" />
            服务器监控会话
          </CardTitle>
          <CardDescription>
            如果这个学校账号也是监控服务使用的账号，可把当前浏览器 token 同步给服务器。
          </CardDescription>
        </div>
        <Badge variant={synced ? 'default' : session.monitorAccount ? 'outline' : 'muted'}>
          {synced ? '已同步' : session.monitorAccount ? '同账号' : '按需同步'}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-3">
        {session.monitorAccount && !synced && (
          <p className="rounded-md border border-status-degraded-border bg-status-degraded-bg px-3 py-2 text-xs text-status-degraded-text">
            本次登录已替换学校设备会话。建议立即同步，否则服务器定时采集会继续使用已失效 token。
          </p>
        )}
        {!open && !synced && (
          <Button variant="outline" onClick={() => setOpen(true)}>
            <IconServer />
            同步到监控服务
          </Button>
        )}
        {open && (
          <div className="flex flex-col gap-2 rounded-md border border-border bg-muted/30 p-3 sm:flex-row sm:items-end">
            <div className="min-w-0 flex-1 space-y-1.5">
              <Label htmlFor="monitor-admin-key">管理密钥</Label>
              <Input
                id="monitor-admin-key"
                type="password"
                autoComplete="off"
                value={adminKey}
                onChange={(event) => setAdminKey(event.target.value)}
                placeholder="输入服务器管理密钥"
              />
            </div>
            <Button disabled={syncing || !adminKey.trim()} onClick={() => void sync()}>
              {syncing ? '同步中...' : '确认同步'}
            </Button>
            <Button
              variant="ghost"
              disabled={syncing}
              onClick={() => {
                setOpen(false);
                setError('');
              }}
            >
              取消
            </Button>
          </div>
        )}
        {error && <p className="text-xs text-destructive">{error}</p>}
        {synced && (
          <p className="text-xs text-status-operational-text">
            服务器已使用当前登录会话，后续请避免再用同一账号在另一台设备登录。
          </p>
        )}
      </CardContent>
    </Card>
  );
}
