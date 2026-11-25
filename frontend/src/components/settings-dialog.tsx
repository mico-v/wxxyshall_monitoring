import * as React from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { IconKey } from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogBody, DialogClose, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { RoomPicker } from '@/components/room-picker';
import { useToast } from '@/components/toast';
import { useAdmin } from '@/lib/admin';
import { useApp } from '@/lib/app-context';
import { roomPath, targetLabel } from '@/lib/constants';
import type { RoomRef, Target } from '@/lib/types';
import { cn } from '@/lib/utils';

interface SettingsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

interface SchoolTokenStatus {
  configured: boolean;
  valid: boolean;
  expired: boolean;
  sno?: string;
  source?: string;
  login_time?: number;
  seconds_left?: number | null;
}

interface AdminSettingsConfig {
  targets: Target[] | null;
  username?: string;
  school_token?: SchoolTokenStatus | null;
}

interface SchoolLoginResponse {
  ok: boolean;
  sno: string;
  expires_in: number;
  expires_in_days: number;
  school_token: SchoolTokenStatus;
}

async function readJSON<T>(response: Response): Promise<T> {
  const body = (await response.json().catch(() => ({}))) as T & { error?: string };
  if (!response.ok) throw new Error(body?.error || `HTTP ${response.status}`);
  return body;
}

function schoolTokenLabel(status: SchoolTokenStatus | null | undefined): string {
  if (!status?.configured) return '未登录';
  if (status.expired || !status.valid) return '登录已过期';
  if (typeof status.seconds_left === 'number') {
    const days = Math.max(0, Math.floor(status.seconds_left / 86400));
    return `剩余 ${days} 天`;
  }
  return '已登录';
}

export function SettingsDialog({ open, onOpenChange }: SettingsDialogProps) {
  const { config, canAdd, room } = useApp();
  const { adminFetch, ensureKey, key, verified } = useAdmin();
  const { toast } = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [draftTargets, setDraftTargets] = React.useState<Target[]>([]);
  const [message, setMessage] = React.useState('');
  const [username, setUsername] = React.useState('');
  const [password, setPassword] = React.useState('');
  const [schoolToken, setSchoolToken] = React.useState<SchoolTokenStatus | null>(null);
  const [loggingIn, setLoggingIn] = React.useState(false);
  const [loginError, setLoginError] = React.useState('');

  React.useEffect(() => {
    if (!open) return;
    let active = true;
    (async () => {
      const allowed = canAdd || (await ensureKey());
      if (!active || !allowed) return;
      try {
        const cfg = await readJSON<AdminSettingsConfig>(await adminFetch('/api/config?admin=1'));
        if (active) {
          setDraftTargets((cfg.targets || []).map((t) => ({ ...t })));
          setUsername(cfg.username || cfg.school_token?.sno || '');
          setSchoolToken(cfg.school_token || null);
        }
      } catch (error) {
        if (active) toast('加载监控宿舍失败: ' + (error as Error).message, true);
      }
    })();
    return () => {
      active = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  React.useEffect(() => {
    if (open) return;
    setPassword('');
    setLoginError('');
  }, [open]);

  const handleSchoolLogin = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const account = username.trim();
    if (!account || !password) {
      setLoginError('请输入学校账号和密码');
      return;
    }
    setLoggingIn(true);
    setLoginError('');
    try {
      const result = await readJSON<SchoolLoginResponse>(
        await adminFetch('/api/school-login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: account, password }),
        }),
      );
      setPassword('');
      setUsername(result.sno || account);
      setSchoolToken(result.school_token);
      void queryClient.invalidateQueries({ queryKey: ['config'] });
      toast(`学校登录成功，有效期约 ${result.expires_in_days} 天`);
    } catch (error) {
      const text = (error as Error).message;
      setLoginError(text);
      toast('学校登录失败: ' + text, true);
    } finally {
      setLoggingIn(false);
    }
  };

  const handleAdded = (added: RoomRef, targets: Target[] | null) => {
    void queryClient.invalidateQueries({ queryKey: ['config'] });
    if (targets) {
      setDraftTargets(targets.map((t) => ({ ...t })));
      return;
    }
    onOpenChange(false);
    void queryClient.invalidateQueries({ queryKey: ['readings'] });
    void navigate({ to: roomPath(added) });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="max-w-2xl">
      <DialogHeader>
        <DialogTitle>查询设置</DialogTitle>
        <DialogClose onClick={() => onOpenChange(false)} />
      </DialogHeader>
      <DialogBody className="space-y-5">
        <section className="border-b border-border pb-5">
          <div className="mb-3 flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <h3 className="text-sm font-semibold">学校账号</h3>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {schoolToken?.sno || '用于获取学校接口 token'}
              </p>
            </div>
            <Badge
              variant="outline"
              className={
                schoolToken?.configured && schoolToken.valid
                  ? 'border-status-operational-border bg-status-operational-bg text-status-operational-text'
                  : schoolToken?.expired
                    ? 'border-status-down-border bg-status-down-bg text-status-down-text'
                    : 'text-muted-foreground'
              }
            >
              {schoolTokenLabel(schoolToken)}
            </Badge>
          </div>

          <form className="space-y-3" onSubmit={(event) => void handleSchoolLogin(event)}>
            <div className="grid gap-3 sm:grid-cols-2">
              <div>
                <Label htmlFor="school-username">学号</Label>
                <Input
                  id="school-username"
                  className="mt-1.5"
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck={false}
                  maxLength={128}
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="school-password">密码</Label>
                <Input
                  id="school-password"
                  className="mt-1.5"
                  type="password"
                  autoComplete="current-password"
                  maxLength={256}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                />
              </div>
            </div>
            {loginError && <p className="text-xs text-destructive">{loginError}</p>}
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="text-xs text-muted-foreground">密码仅用于本次登录，不会保存。</span>
              <Button type="submit" size="sm" disabled={loggingIn}>
                <IconKey />
                {loggingIn ? '正在登录…' : '登录获取 Token'}
              </Button>
            </div>
          </form>
        </section>

        <section>
          <h3 className="mb-2 text-xs font-semibold text-muted-foreground">已监控宿舍</h3>
          <div className="flex flex-col gap-2">
            {draftTargets.length === 0 && (
              <p className="text-xs text-muted-foreground">还没有监控宿舍,在下面添加。</p>
            )}
            {draftTargets.map((t) => (
              <div
                key={`${t.campus}|${t.building}|${t.room}`}
                className={cn(
                  'flex items-center gap-2 rounded-md border border-border bg-muted px-3 py-2 text-sm',
                  room &&
                    t.campus === room.campus &&
                    t.building === room.building &&
                    t.room === room.room &&
                    'border-ring/60',
                )}
              >
                <span className="min-w-0 flex-1 truncate">{targetLabel(t)}</span>
                <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                  {t.campus}/{t.building}/{t.room}
                </span>
              </div>
            ))}
          </div>
        </section>

        {config && (
          <RoomPicker
            defaults={config.defaults}
            guestMode={!key && !verified}
            onAdded={handleAdded}
            onStatus={setMessage}
          />
        )}
        {message && <p className="text-xs text-muted-foreground">{message}</p>}
      </DialogBody>
    </Dialog>
  );
}
