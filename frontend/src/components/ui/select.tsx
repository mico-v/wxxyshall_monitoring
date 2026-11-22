import * as React from 'react';
import { cn } from '@/lib/utils';

/** 原生 select，视觉对齐 shadcn。 */
export function Select({ className, children, ...props }: React.ComponentProps<'select'>) {
  return (
    <select
      className={cn(
        'select-control flex h-10 w-full min-w-0 cursor-pointer rounded-md border border-input bg-card px-3 py-1 pr-9 text-sm font-medium text-foreground shadow-xs transition-[border-color,box-shadow,background-color] outline-none hover:border-ring/70 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:bg-muted/50 disabled:font-normal disabled:opacity-70',
        className,
      )}
      {...props}
    >
      {children}
    </select>
  );
}
