import * as React from 'react';
import { ADMIN_KEY_STORE } from './constants';
import { verifyAdminKey } from './api';
import { useToast } from '@/components/toast';
import { Dialog, DialogBody, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Button } from '@/components/ui/button';

interface AdminState {
  /** 已保存的管理密钥（可能尚未校验）。 */
  key: string;
  /** 手里的密钥已通过 /api/admin/verify 校验。 */
  verified: boolean;
  /** 启动时的密钥校验已完成（无密钥则立即就绪），此前不发带鉴权的请求。 */
  ready: boolean;
}

interface AdminContextValue extends AdminState {
  hasKey: boolean;
  setKey: (key: string) => void;
  setVerified: (verified: boolean) => void;
  adminFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
  /** 管理鉴权开启时要求密钥，关闭时直接放行。 */
  ensureAdmin: (required: boolean) => Promise<boolean>;
  /** 强制获取有效密钥（隐藏主页解锁、访客不允许添加时）。 */
  ensureKey: () => Promise<boolean>;
  /** 用现有密钥校验一次，成功即视为已登录。 */
  verify: (key: string) => Promise<boolean>;
  logout: () => void;
}

const AdminContext = React.createContext<AdminContextValue | null>(null);

export function useAdmin(): AdminContextValue {
  const context = React.useContext(AdminContext);
  if (!context) throw new Error('useAdmin must be used within AdminProvider');
  return context;
}

function loadStoredKey(): string {
  try {
    const stored = localStorage.getItem(ADMIN_KEY_STORE);
    if (stored) return stored;
    const legacy = sessionStorage.getItem(ADMIN_KEY_STORE);
    if (legacy) {
      localStorage.setItem(ADMIN_KEY_STORE, legacy);
      sessionStorage.removeItem(ADMIN_KEY_STORE);
      return legacy;
    }
  } catch (error) {
    console.warn('读取管理密钥失败:', error);
  }
  return '';
}

/** 允许 /?key=... 直接打开管理页面，读取后立即从地址栏移除。 */
function consumeUrlKey(): string {
  try {
    const url = new URL(window.location.href);
    const fromUrl = url.searchParams.get('key');
    if (fromUrl && fromUrl.trim()) {
      url.searchParams.delete('key');
      window.history.replaceState(window.history.state, '', url.pathname + url.search + url.hash);
      return fromUrl.trim();
    }
  } catch {
    /* ignore */
  }
  return '';
}

export function AdminProvider({ children }: { children: React.ReactNode }) {
  const { toast } = useToast();
  const [state, setState] = React.useState<AdminState>(() => {
    const seeded = consumeUrlKey();
    const stored = seeded || loadStoredKey();
    if (seeded) {
      try {
        localStorage.setItem(ADMIN_KEY_STORE, seeded);
      } catch {
        /* ignore */
      }
    }
    return { key: stored, verified: false, ready: false };
  });

  const [promptOpen, setPromptOpen] = React.useState(false);
  const promptResolver = React.useRef<((value: string) => void) | null>(null);
  const [draft, setDraft] = React.useState('');

  const persist = React.useCallback((key: string) => {
    try {
      if (key) localStorage.setItem(ADMIN_KEY_STORE, key);
      else localStorage.removeItem(ADMIN_KEY_STORE);
    } catch (error) {
      console.warn('保存管理密钥失败:', error);
    }
  }, []);

  const setKey = React.useCallback(
    (key: string) => {
      setState((prev) => ({ key, verified: key ? prev.verified : false, ready: prev.ready }));
      persist(key);
    },
    [persist],
  );

  const setVerified = React.useCallback((verified: boolean) => {
    setState((prev) => ({ ...prev, verified }));
  }, []);

  const verify = React.useCallback(
    async (key: string): Promise<boolean> => {
      const trimmed = key.trim();
      if (!trimmed) return false;
      try {
        const ok = await verifyAdminKey(trimmed);
        if (ok) {
          setState({ key: trimmed, verified: true, ready: true });
          persist(trimmed);
        } else {
          setState((prev) =>
            prev.key === trimmed ? { key: '', verified: false, ready: true } : prev,
          );
          persist('');
        }
        return ok;
      } catch (error) {
        // 网络/服务异常原样抛出，避免误清空有效密钥或陷入反复弹窗。
        throw error;
      }
    },
    [persist],
  );

  // 启动时校验已保存的长效密钥。
  const initialKeyRef = React.useRef(state.key);
  React.useEffect(() => {
    const key = initialKeyRef.current;
    if (!key) {
      setState((prev) => ({ ...prev, ready: true }));
      return;
    }
    let active = true;
    verifyAdminKey(key)
      .then((ok) => {
        if (!active) return;
        if (ok) setState({ key, verified: true, ready: true });
        else {
          setState({ key: '', verified: false, ready: true });
          persist('');
        }
      })
      .catch((error) => {
        console.warn('校验已保存的管理密钥失败:', error);
        if (active) setState((prev) => ({ ...prev, ready: true }));
      });
    return () => {
      active = false;
    };
  }, [persist]);

  const adminFetch = React.useCallback(
    async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const headers = new Headers(init?.headers || (input instanceof Request ? input.headers : undefined));
      const sentKey = !!state.key;
      if (state.key) headers.set('Authorization', `Bearer ${state.key}`);
      const response = await fetch(input, { ...init, headers, cache: init?.cache ?? 'no-store' });
      if (response.status === 401 && sentKey) {
        // 仅当 401 来自管理密钥校验（realm=elec-admin）且本次确实带了密钥时才清除登录态；
        // 校区发现因缺少学校 token 返回 401、或本请求未带密钥时，不应把管理员登出。
        const challenge = response.headers.get('WWW-Authenticate') || '';
        if (challenge.includes('elec-admin')) {
          setState((prev) => ({ key: '', verified: false, ready: prev.ready }));
          persist('');
        }
      }
      return response;
    },
    [state.key, persist],
  );

  const requestKey = React.useCallback((): Promise<string> => {
    if (promptResolver.current) return Promise.resolve('');
    setDraft('');
    setPromptOpen(true);
    return new Promise<string>((resolve) => {
      promptResolver.current = resolve;
    });
  }, []);

  const finishPrompt = React.useCallback((value: string) => {
    setPromptOpen(false);
    const resolve = promptResolver.current;
    promptResolver.current = null;
    resolve?.(value);
  }, []);

  const ensureKey = React.useCallback(async (): Promise<boolean> => {
    if (state.key) {
      try {
        if (await verify(state.key)) return true;
      } catch (error) {
        console.warn('校验管理密钥失败:', error);
      }
    }
    for (;;) {
      const entered = (await requestKey()).trim();
      if (!entered) return false;
      try {
        if (await verify(entered)) return true;
        toast('管理密钥无效，请重新输入', true);
      } catch (error) {
        toast('校验失败: ' + (error as Error).message, true);
        return false;
      }
    }
  }, [state.key, verify, requestKey, toast]);

  const ensureAdmin = React.useCallback(
    async (required: boolean): Promise<boolean> => {
      if (!required) return true;
      return ensureKey();
    },
    [ensureKey],
  );

  const logout = React.useCallback(() => {
    setState((prev) => ({ key: '', verified: false, ready: prev.ready }));
    persist('');
  }, [persist]);

  const value = React.useMemo<AdminContextValue>(
    () => ({
      ...state,
      hasKey: !!state.key,
      setKey,
      setVerified,
      adminFetch,
      ensureAdmin,
      ensureKey,
      verify,
      logout,
    }),
    [state, setKey, setVerified, adminFetch, ensureAdmin, ensureKey, verify, logout],
  );

  return (
    <AdminContext.Provider value={value}>
      {children}
      <Dialog open={promptOpen} onOpenChange={(open) => !open && finishPrompt('')}>
        <DialogHeader>
          <DialogTitle>管理鉴权</DialogTitle>
        </DialogHeader>
        <DialogBody>
          <Label htmlFor="admin-key-input">管理密钥</Label>
          <Input
            id="admin-key-input"
            type="password"
            autoComplete="new-password"
            maxLength={256}
            spellCheck={false}
            autoFocus
            className="mt-1.5"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault();
                finishPrompt(draft.trim());
              }
            }}
          />
          <p className="mt-2 text-xs text-muted-foreground">
            密钥验证后长期保存在本浏览器，可随时「退出登录」清除。
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="outline" onClick={() => finishPrompt('')}>
              取消
            </Button>
            <Button onClick={() => finishPrompt(draft.trim())}>验证</Button>
          </div>
        </DialogBody>
      </Dialog>
    </AdminContext.Provider>
  );
}
