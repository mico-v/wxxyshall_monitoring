import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { ToastProvider } from '@/components/toast';
import { AdminProvider } from '@/lib/admin';
import { router } from '@/router';
import './styles.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch((error) => {
    console.warn('SW 注册失败:', error);
  });
}

const container = document.getElementById('root');
if (container) {
  createRoot(container).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <AdminProvider>
            <RouterProvider router={router} />
          </AdminProvider>
        </ToastProvider>
      </QueryClientProvider>
    </StrictMode>,
  );
}
