import { THEME_KEY } from './constants';

export function applyTheme(dark: boolean): void {
  document.documentElement.classList.toggle('dark', dark);
}

export function storedThemeIsDark(): boolean {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved === 'dark') return true;
    if (saved === 'light') return false;
  } catch {
    /* ignore */
  }
  return matchMedia('(prefers-color-scheme: dark)').matches;
}

export function persistTheme(dark: boolean): void {
  try {
    localStorage.setItem(THEME_KEY, dark ? 'dark' : 'light');
  } catch {
    /* ignore */
  }
}
