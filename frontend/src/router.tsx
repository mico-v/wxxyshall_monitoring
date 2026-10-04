import { createRouter } from '@tanstack/react-router';
import { indexRoute } from '@/routes/index';
import { roomRoute } from '@/routes/room';
import { rootRoute } from '@/routes/__root';

const routeTree = rootRoute.addChildren([indexRoute, roomRoute]);

export const router = createRouter({
  routeTree,
  defaultPreload: 'intent',
  scrollRestoration: true,
});

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
