import * as React from 'react';
import { IconArrowRight, IconBuilding, IconDoor, IconMapPin } from '@tabler/icons-react';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Select } from '@/components/ui/select';
import { useUserOptions } from '@/lib/user-cache';

export function UserRoomPicker({
  onSelect,
  busy = false,
}: {
  onSelect: (room: { campus: string; building: string; room: string }) => void;
  busy?: boolean;
}) {
  const [campus, setCampus] = React.useState('');
  const [building, setBuilding] = React.useState('');
  const [room, setRoom] = React.useState('');

  const campuses = useUserOptions('campuses');
  const buildings = useUserOptions('buildings', { campus }, Boolean(campus));
  const rooms = useUserOptions('rooms', { campus, building }, Boolean(campus && building));

  const campusOptions = campuses.data ?? [];
  const buildingOptions = buildings.data ?? [];
  const roomOptions = rooms.data ?? [];
  const selectedCampus = campusOptions.find((option) => option.value === campus);
  const selectedBuilding = buildingOptions.find((option) => option.value === building);
  const selectedRoom = roomOptions.find((option) => option.value === room);
  const error = campuses.error || buildings.error || rooms.error;
  const loading = campuses.isLoading || buildings.isLoading || rooms.isLoading;

  function selectCampus(value: string) {
    setCampus(value);
    setBuilding('');
    setRoom('');
  }

  function selectBuilding(value: string) {
    setBuilding(value);
    setRoom('');
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-3 md:grid-cols-3">
        <div className="space-y-1.5">
          <Label htmlFor="user-campus" className="inline-flex items-center gap-1.5">
            <IconMapPin className="size-3.5" />
            校区
          </Label>
          <Select
            id="user-campus"
            value={campus}
            disabled={campuses.isLoading}
            onChange={(event) => selectCampus(event.target.value)}
          >
            <option value="">{campuses.isLoading ? '加载中...' : '选择校区'}</option>
            {campusOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.name}
              </option>
            ))}
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="user-building" className="inline-flex items-center gap-1.5">
            <IconBuilding className="size-3.5" />
            楼栋
          </Label>
          <Select
            id="user-building"
            value={building}
            disabled={!campus || buildings.isLoading}
            onChange={(event) => selectBuilding(event.target.value)}
          >
            <option value="">{buildings.isLoading ? '加载中...' : '选择楼栋'}</option>
            {buildingOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.name}
              </option>
            ))}
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="user-room" className="inline-flex items-center gap-1.5">
            <IconDoor className="size-3.5" />
            房间
          </Label>
          <Select
            id="user-room"
            value={room}
            disabled={!building || rooms.isLoading}
            onChange={(event) => setRoom(event.target.value)}
          >
            <option value="">{rooms.isLoading ? '加载中...' : '选择房间'}</option>
            {roomOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.name}
              </option>
            ))}
          </Select>
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="min-h-5 text-xs text-muted-foreground">
          {error
            ? `查询失败：${(error as Error).message}`
            : campus && building && room
              ? [selectedCampus?.name, selectedBuilding?.name, selectedRoom?.name]
                  .filter(Boolean)
                  .join(' / ')
              : '选择房间后查询余额和充值记录'}
        </p>
        <Button
          disabled={!campus || !building || !room || loading || busy}
          onClick={() => onSelect({ campus, building, room })}
        >
          查询房间
          <IconArrowRight />
        </Button>
      </div>
    </div>
  );
}
