import * as React from 'react';
import { Link, Outlet, createRootRoute, useRouterState } from '@tanstack/react-router';
import { IconCreditCard } from '@tabler/icons-react';
import { PageFooter } from '@/components/page-footer';
import { PageHeader } from '@/components/page-header';
import { ThemeToggle } from '@/components/theme-toggle';
import { SettingsDialog } from '@/components/settings-dialog';
import { AdminProvider, useAdmin } from '@/lib/admin';
import { ApiError } from '@/lib/api';
import { AppContextProvider, type AppContextValue } from '@/lib/app-context';
import { usePublicConfig } from '@/lib/hooks';
import { roomFromPathname } from '@/lib/route';
import { useUser, UserProvider } from '@/lib/user-context';

export const rootRoute = createRootRoute({ component: RootLayout });

function RootLayout() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  if (pathname === '/user' || pathname.startsWith('/user/')) {
    return (
      <UserProvider>
        <UserPortalLayout />
      </UserProvider>
    );
  }
  return <AdminLayout />;
}

function UserPortalLayout() {
  const { session, logout } = useUser();
  React.useEffect(() => {
    const previous = document.title;
    document.title = '校园卡服务 · 宿舍电费监控';
    return () => {
      document.title = previous;
    };
  }, []);
  return (
    <div className="flex min-h-screen flex-col">
      <header className="sticky top-0 z-40 border-b border-border bg-background/80 backdrop-blur-sm">
        <div className="mx-auto flex min-h-16 w-full max-w-5xl flex-wrap items-center justify-between gap-2 px-4 py-2">
          <Link to="/user" className="flex min-w-0 items-center gap-2">
            <span className="flex size-8 items-center justify-center rounded-md bg-primary/10 text-primary">
              <IconCreditCard className="size-5" />
            </span>
            <span className="truncate text-lg font-semibold">校园卡服务</span>
          </Link>
          <div className="flex flex-wrap items-center gap-2">
            <Link to="/" className="text-sm text-muted-foreground hover:text-foreground">
              电费监控
            </Link>
            {session && (
              <button
                type="button"
                className="rounded-md px-2.5 py-1.5 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
                onClick={logout}
              >
                退出登录
              </button>
            )}
            <ThemeToggle />
          </div>
        </div>
      </header>
      <main className="flex-1">
        <Outlet />
      </main>
      <PageFooter />
    </div>
  );
}

function AdminLayout() {
  return (
    <AdminProvider>
      <AdminPageLayout />
    </AdminProvider>
  );
}

function AdminPageLayout() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const room = React.useMemo(() => roomFromPathname(pathname), [pathname]);
  const configQuery = usePublicConfig(room);
  const { verified, key: adminKey, setKey } = useAdmin();
  const config = configQuery.data;
  const [settingsOpen, setSettingsOpen] = React.useState(false);

  // 密钥被服务端轮换/清空后，带密钥的 /api/config 会 401；此时清掉本地密钥
  // 回退到公开配置，避免整个主页停在错误态。
  React.useEffect(() => {
    const error = configQuery.error as ApiError | null;
    if (error?.status === 401 && adminKey) setKey('');
  }, [configQuery.error, adminKey, setKey]);

  const aggregate = !!room || verified;
  const picker = !room && !aggregate;
  const showCollectButton = verified;

  const value = React.useMemo<AppContextValue>(
    () => ({
      room,
      config,
      aggregate,
      picker,
      canAdd: verified || config?.allow_public_add_target === true,
      showCollectButton,
      targets: config?.targets ?? [],
    }),
    [room, config, aggregate, picker, verified, showCollectButton],
  );

  const waitingForConfig = !room && !config && configQuery.isLoading;

  return (
    <AppContextProvider value={value}>
      <div className="flex min-h-screen flex-col">
        <PageHeader onOpenSettings={() => setSettingsOpen(true)} />
        <main className="flex-1">
          {waitingForConfig ? (
            <div className="flex h-64 items-center justify-center text-sm text-muted-foreground">
              加载中…
            </div>
          ) : (
            <Outlet />
          )}
        </main>
        <PageFooter />
      </div>
      <SettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
    </AppContextProvider>
  );
}
