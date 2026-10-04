import { useEffect, useState } from 'react';
import { IconMoon, IconSun } from '@tabler/icons-react';
import { Button } from '@/components/ui/button';
import { applyTheme, persistTheme } from '@/lib/theme';

function currentDark(): boolean {
  return document.documentElement.classList.contains('dark');
}

export function ThemeToggle() {
  const [dark, setDark] = useState(currentDark);

  useEffect(() => {
    const observer = new MutationObserver(() => setDark(currentDark()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    return () => observer.disconnect();
  }, []);

  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={dark ? '切换到浅色' : '切换到深色'}
      title={dark ? '浅色' : '深色'}
      onClick={() => {
        const next = !currentDark();
        applyTheme(next);
        persistTheme(next);
        setDark(next);
      }}
    >
      {dark ? <IconSun /> : <IconMoon />}
    </Button>
  );
}
