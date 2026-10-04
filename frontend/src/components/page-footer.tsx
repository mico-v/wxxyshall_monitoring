import { IconBolt } from '@tabler/icons-react';
import { PAGE_CONTAINER_CLASSES } from '@/lib/constants';

export function PageFooter() {
  const version = document.querySelector('meta[name="app-version"]')?.getAttribute('content') || '';
  const versionLabel = version && version !== 'dev' ? ` · ${version}` : '';
  return (
    <footer className="mt-auto border-t border-border bg-muted/40">
      <div className={`${PAGE_CONTAINER_CLASSES} flex flex-wrap items-center justify-between gap-2 py-4 text-xs text-muted-foreground`}>
        <span className="inline-flex items-center gap-1.5">
          <IconBolt className="size-4 text-primary" aria-hidden="true" />
          宿舍电费监控
        </span>
        <a
          href="https://github.com/mico-v/wxxyshall_monitoring"
          target="_blank"
          rel="noopener noreferrer"
          className="hover:text-foreground"
        >
          github.com/mico-v/wxxyshall_monitoring
          {versionLabel}
        </a>
      </div>
    </footer>
  );
}
