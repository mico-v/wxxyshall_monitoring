import * as React from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  clearUserSession,
  loadUserSession,
  saveUserSession,
  type UserSession,
} from './user-store';
import {
  getLoginKeyboard,
  loginWithMappedPassword,
  mapKeyboardPassword,
  readUserJSON,
} from './user-api';

interface UserContextValue {
  ready: boolean;
  session: UserSession | null;
  login: (username: string, password: string, confirmMonitorSession?: boolean) => Promise<void>;
  logout: () => void;
  syncToServer: (adminKey: string) => Promise<void>;
  userFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
}

const UserContext = React.createContext<UserContextValue | null>(null);

export function useUser(): UserContextValue {
  const context = React.useContext(UserContext);
  if (!context) throw new Error('useUser must be used within UserProvider');
  return context;
}

export function UserProvider({ children }: { children: React.ReactNode }) {
  const queryClient = useQueryClient();
  const [ready, setReady] = React.useState(false);
  const [session, setSession] = React.useState<UserSession | null>(null);

  React.useEffect(() => {
    let active = true;
    loadUserSession()
      .then((stored) => {
        if (active) setSession(stored);
      })
      .catch((error) => {
        console.warn('恢复本地登录状态失败:', error);
      })
      .finally(() => {
        if (active) setReady(true);
      });
    return () => {
      active = false;
    };
  }, []);

  const clearSession = React.useCallback(async () => {
    setSession(null);
    queryClient.clear();
    try {
      await clearUserSession();
    } catch (error) {
      console.warn('清除本地登录状态失败:', error);
    }
  }, [queryClient]);

  const login = React.useCallback(
    async (username: string, password: string, confirmMonitorSession = false) => {
      const account = username.trim();
      if (!account || !password) throw new Error('请输入学校账号和密码');
      const keyboard = await getLoginKeyboard();
      const mapped = mapKeyboardPassword(password, keyboard.keyboard);
      const result = await loginWithMappedPassword(
        keyboard.session_id,
        account,
        mapped,
        confirmMonitorSession,
      );
      const next: UserSession = {
        sno: result.sno || account,
        accessToken: result.access_token,
        expiresIn: result.expires_in,
        loginTime: result.login_time,
        source: result.source || 'browser',
        monitorAccount: result.monitor_account === true,
      };
      await saveUserSession(next);
      queryClient.clear();
      setSession(next);
    },
    [queryClient],
  );

  const userFetch = React.useCallback(
    async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const headers = new Headers(init?.headers || (input instanceof Request ? input.headers : undefined));
      if (session?.accessToken) headers.set('X-School-Token', session.accessToken);
      const response = await fetch(input, {
        ...init,
        headers,
        cache: init?.cache ?? 'no-store',
      });
      if (response.status === 401 && session?.accessToken) {
        await clearSession();
      }
      return response;
    },
    [session?.accessToken, clearSession],
  );

  const logout = React.useCallback(() => {
    void clearSession();
  }, [clearSession]);

  const syncToServer = React.useCallback(
    async (adminKey: string) => {
      if (!session) throw new Error('请先登录学校账号');
      const key = adminKey.trim();
      if (!key) throw new Error('请输入管理密钥');
      await readUserJSON<{ ok: boolean }>(
        await fetch('/api/token', {
          method: 'POST',
          cache: 'no-store',
          headers: {
            Authorization: `Bearer ${key}`,
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            access_token: session.accessToken,
            expires_in: session.expiresIn,
            login_time: session.loginTime,
            sno: session.sno,
            source: 'user-portal-sync',
          }),
        }),
      );
    },
    [session],
  );

  const value = React.useMemo<UserContextValue>(
    () => ({ ready, session, login, logout, syncToServer, userFetch }),
    [ready, session, login, logout, syncToServer, userFetch],
  );

  return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
