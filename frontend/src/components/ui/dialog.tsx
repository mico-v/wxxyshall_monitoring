import * as React from 'react';
import { createPortal } from 'react-dom';
import { IconX } from '@tabler/icons-react';
import { cn } from '@/lib/utils';
import { Button } from './button';

interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
  /** 点击遮罩与 Esc 是否可关闭（管理密钥验证等场景同样允许取消）。 */
  dismissible?: boolean;
  className?: string;
}

export function Dialog({
  open,
  onOpenChange,
  children,
  dismissible = true,
  className,
}: DialogProps) {
  React.useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (dismissible && event.key === 'Escape') onOpenChange(false);
    };
    document.addEventListener('keydown', onKey);
    const previous = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = previous;
    };
  }, [open, dismissible, onOpenChange]);

  if (!open) return null;

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/50 p-4 pt-[8vh] backdrop-blur-[2px]"
      onMouseDown={(event) => {
        if (dismissible && event.target === event.currentTarget) onOpenChange(false);
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        className={cn(
          'relative w-full max-w-lg overflow-hidden rounded-lg border border-border bg-card text-card-foreground shadow-xl',
          className,
        )}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
}

export function DialogHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn(
        'sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-border bg-card px-4 py-3',
        className,
      )}
      {...props}
    />
  );
}

export function DialogTitle({ className, ...props }: React.ComponentProps<'h2'>) {
  return <h2 className={cn('text-base font-semibold', className)} {...props} />;
}

export function DialogBody({ className, ...props }: React.ComponentProps<'div'>) {
  return <div className={cn('px-4 py-4', className)} {...props} />;
}

export function DialogClose({ onClick }: { onClick: () => void }) {
  return (
    <Button variant="ghost" size="icon-sm" aria-label="关闭" onClick={onClick}>
      <IconX />
    </Button>
  );
}
