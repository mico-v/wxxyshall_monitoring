import * as React from 'react';
import { createRoute } from '@tanstack/react-router';
import { Dashboard } from '@/components/dashboard';
import { PAGE_CONTAINER_CLASSES } from '@/lib/constants';
import { validateDaysSearch } from '@/lib/search';
import type { RoomRef } from '@/lib/types';
import { rootRoute } from './__root';

export const roomRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/room/$campus/$building/$room',
  validateSearch: validateDaysSearch,
  component: RoomPage,
});

function RoomPage() {
  const params = roomRoute.useParams();
  const { days } = roomRoute.useSearch();
  const navigate = roomRoute.useNavigate();

  const room = React.useMemo<RoomRef>(
    () => ({ campus: params.campus, building: params.building, room: params.room }),
    [params.campus, params.building, params.room],
  );

  return (
    <div className={PAGE_CONTAINER_CLASSES}>
      <Dashboard
        room={room}
        days={days ?? 0}
        onDaysChange={(next) =>
          void navigate({ search: (prev) => ({ ...prev, days: next }), replace: true })
        }
      />
    </div>
  );
}
