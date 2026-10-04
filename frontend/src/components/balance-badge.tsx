import {
  IconAlertTriangle,
  IconCircleCheck,
  IconCircleDashed,
  IconCircleX,
} from '@tabler/icons-react';
import { cn } from '@/lib/utils';
import type { BalanceLevel, BalanceStatus } from '@/lib/format';

const STYLES: Record<string, string> = {
  good: 'bg-status-operational-bg border-status-operational-border text-status-operational-text',
  warning: 'bg-status-degraded-bg border-status-degraded-border text-status-degraded-text',
  serious: 'bg-status-serious-bg border-status-serious-border text-status-serious-text',
  critical: 'bg-status-down-bg border-status-down-border text-status-down-text',
};

export function BalanceBadge({ status, className }: { status: BalanceStatus; className?: string }) {
  if (!status.level) return null;
  return (
    <span
      className={cn(
        'inline-flex w-fit items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap',
        STYLES[status.level],
        className,
      )}
    >
      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
      {status.text}
    </span>
  );
}

const ICONS = {
  good: [IconCircleCheck, 'text-status-operational'],
  warning: [IconAlertTriangle, 'text-status-degraded'],
  serious: [IconAlertTriangle, 'text-status-serious'],
  critical: [IconCircleX, 'text-status-down'],
  '': [IconCircleDashed, 'text-status-unknown'],
} as const;

/** 与余额分级对应的状态图标（颜色同状态徽标）。 */
export function BalanceIcon({ level, className }: { level: BalanceLevel; className?: string }) {
  const [Icon, color] = ICONS[level];
  return <Icon aria-hidden="true" className={cn('size-5 shrink-0', color, className)} />;
}
