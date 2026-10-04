import * as React from 'react';
import { Link } from '@tanstack/react-router';
import { IconBolt, IconLogout, IconSettings } from '@tabler/icons-react';
import { Button } from '@/components/ui/button';
import { CollectButton } from '@/components/collect-button';
import { ThemeToggle } from '@/components/theme-toggle';
import { useToast } from '@/components/toast';
import { useAdmin } from '@/lib/admin';
import { useApp } from '@/lib/app-context';
import { PAGE_CONTAINER_CLASSES } from '@/lib/constants';

export function PageHeader({ onOpenSettings }: { onOpenSettings: () => void }) {
  const { picker, canAdd } = useApp();
  const { hasKey, ensureKey, logout } = useAdmin();
  const { toast } = useToast();
  const [openingSettings, setOpeningSettings] = React.useState(false);

  // 与旧版一致：无添加权限（未登录且禁访客添加）时先弹密钥验证，
  // 校验通过或已有有效密钥才打开「查询设置」；取消则什么都不做。
  async function handleOpenSettings() {
    if (openingSettings) return;
    if (!canAdd) {
      setOpeningSettings(true);
      try {
        if (!(await ensureKey())) return;
      } finally {
        setOpeningSettings(false);
      }
    }
    onOpenSettings();
  }

  return (
    <header className="sticky top-0 z-40 border-b border-border bg-background/80 backdrop-blur-sm">
      <div
        className={`${PAGE_CONTAINER_CLASSES} flex min-h-16 flex-wrap items-center justify-between gap-2 py-2`}
      >
        <Link to="/" className="flex min-w-0 items-center gap-2">
          <IconBolt className="size-6 shrink-0 text-primary" aria-hidden="true" />
          <span className="truncate text-lg font-semibold text-foreground">宿舍电费监控</span>
        </Link>

        <div className="flex flex-wrap items-center gap-2">
          {!picker && (
            <>
              <CollectButton />
              <Button
                variant="outline"
                size="sm"
                onClick={() => void handleOpenSettings()}
                disabled={openingSettings}
              >
                <IconSettings />
                查询设置
              </Button>
            </>
          )}
          {hasKey && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                logout();
                toast('已退出登录');
              }}
            >
              <IconLogout />
              退出登录
            </Button>
          )}
          <ThemeToggle />
        </div>
      </div>
    </header>
  );
}
