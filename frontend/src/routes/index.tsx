import { createRoute } from '@tanstack/react-router';
import { Dashboard } from '@/components/dashboard';
import { HomePicker } from '@/components/home-picker';
import { useApp } from '@/lib/app-context';
import { PAGE_CONTAINER_CLASSES } from '@/lib/constants';
import { validateDaysSearch } from '@/lib/search';
import { rootRoute } from './__root';

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  validateSearch: validateDaysSearch,
  component: HomePage,
});

function HomePage() {
  const { days } = indexRoute.useSearch();
  const navigate = indexRoute.useNavigate();
  const { picker } = useApp();

  return (
    <div className={PAGE_CONTAINER_CLASSES}>
      {picker ? (
        <HomePicker />
      ) : (
        <Dashboard
          room={null}
          days={days ?? 0}
          onDaysChange={(next) =>
            void navigate({ search: (prev) => ({ ...prev, days: next }), replace: true })
          }
        />
      )}
    </div>
  );
}
