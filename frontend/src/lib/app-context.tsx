import * as React from 'react';
import type { PublicConfig, RoomRef, Target } from './types';

export interface AppContextValue {
  room: RoomRef | null;
  config: PublicConfig | undefined;
  /** 聚合数据（全部宿舍）可读：主页公开，或已登录，或在宿舍页。 */
  aggregate: boolean;
  /** 主页隐藏且未登录：显示宿舍选择器落地页。 */
  picker: boolean;
  canAdd: boolean;
  targets: Target[];
}

const AppContext = React.createContext<AppContextValue | null>(null);

export function AppContextProvider({
  value,
  children,
}: {
  value: AppContextValue;
  children: React.ReactNode;
}) {
  return <AppContext.Provider value={value}>{children}</AppContext.Provider>;
}

export function useApp(): AppContextValue {
  const context = React.useContext(AppContext);
  if (!context) throw new Error('useApp must be used within AppContextProvider');
  return context;
}
