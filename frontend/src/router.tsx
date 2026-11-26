import { createRouter } from '@tanstack/react-router';
import { indexRoute } from '@/routes/index';
import { roomRoute } from '@/routes/room';
import { userRoute } from '@/routes/user';
import { userRoomRoute } from '@/routes/user-room';
import { rootRoute } from '@/routes/__root';

const routeTree = rootRoute.addChildren([indexRoute, roomRoute, userRoute, userRoomRoute]);

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
