import type { RoomRef } from './types';

const ROOM_PATH = /^\/room\/([^/]+)\/([^/]+)\/([^/]+)\/?$/;

export function roomFromPathname(pathname: string): RoomRef | null {
  const match = pathname.match(ROOM_PATH);
  if (!match) return null;
  const [, campus, building, room] = match;
  if (!campus || !building || !room) return null;
  return {
    campus: decodeURIComponent(campus),
    building: decodeURIComponent(building),
    room: decodeURIComponent(room),
  };
}
