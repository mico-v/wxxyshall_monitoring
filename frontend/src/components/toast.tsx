import * as React from 'react';
import { cn } from '@/lib/utils';

interface ToastItem {
  id: number;
  message: string;
  error: boolean;
}

interface ToastContextValue {
  toast: (message: string, error?: boolean) => void;
}

const ToastContext = React.createContext<ToastContextValue | null>(null);

export function useToast(): ToastContextValue {
  const context = React.useContext(ToastContext);
  if (!context) throw new Error('useToast must be used within ToastProvider');
  return context;
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = React.useState<ToastItem[]>([]);
  const idRef = React.useRef(0);

  const toast = React.useCallback((message: string, error = false) => {
    const id = ++idRef.current;
    setItems((prev) => [...prev, { id, message, error }]);
    window.setTimeout(() => {
      setItems((prev) => prev.filter((item) => item.id !== id));
    }, 3200);
  }, []);

  const value = React.useMemo(() => ({ toast }), [toast]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        role="status"
        aria-live="polite"
        className="pointer-events-none fixed inset-x-0 bottom-6 z-100 flex flex-col items-center gap-2 px-4"
      >
        {items.map((item) => (
          <div
            key={item.id}
            className={cn(
              'pointer-events-auto max-w-[80vw] rounded-md px-4 py-2 text-sm font-medium shadow-lg',
              item.error
                ? 'bg-destructive text-white'
                : 'bg-foreground text-background',
            )}
          >
            {item.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
