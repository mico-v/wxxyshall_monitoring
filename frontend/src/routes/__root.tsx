import * as React from 'react';
import { Outlet, createRootRoute, useRouterState } from '@tanstack/react-router';
import { PageFooter } from '@/components/page-footer';
import { PageHeader } from '@/components/page-header';
import { SettingsDialog } from '@/components/settings-dialog';
import { useAdmin } from '@/lib/admin';
import { ApiError } from '@/lib/api';
import { AppContextProvider, type AppContextValue } from '@/lib/app-context';
import { usePublicConfig } from '@/lib/hooks';
import { roomFromPathname } from '@/lib/route';

export const rootRoute = createRootRoute({ component: RootLayout });

function RootLayout() {
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

  const aggregate = !!room || config?.show_homepage !== false || verified;
  const picker = !room && !aggregate;

  const value = React.useMemo<AppContextValue>(
    () => ({
      room,
      config,
      aggregate,
      picker,
      canAdd: verified || config?.guest_add_allowed === true,
      targets: config?.targets ?? [],
    }),
    [room, config, aggregate, picker, verified],
  );

  // 主页需要配置来判定 show_homepage；宿舍页数据公开，可先渲染。
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
