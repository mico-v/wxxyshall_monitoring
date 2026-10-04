import { cn } from '@/lib/utils';

export interface ToggleOption<T extends string | number> {
  value: T;
  label: string;
}

interface ToggleGroupProps<T extends string | number> {
  options: readonly ToggleOption<T>[];
  value: T;
  onChange: (value: T) => void;
  className?: string;
  'aria-label'?: string;
}

/** shadcn 风格的分段控件（toggle-group）。 */
export function ToggleGroup<T extends string | number>({
  options,
  value,
  onChange,
  className,
  'aria-label': ariaLabel,
}: ToggleGroupProps<T>) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={cn(
        'inline-flex items-center gap-0.5 rounded-md border border-border bg-muted p-0.5',
        className,
      )}
    >
      {options.map((option) => (
        <button
          key={String(option.value)}
          type="button"
          aria-pressed={option.value === value}
          onClick={() => onChange(option.value)}
          className={cn(
            'rounded-sm px-3 py-1 text-xs font-medium whitespace-nowrap transition-colors',
            option.value === value
              ? 'bg-card text-foreground shadow-xs'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
