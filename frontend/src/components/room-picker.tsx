import * as React from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select } from '@/components/ui/select';
import { useAdmin } from '@/lib/admin';
import type { RoomRef, Target } from '@/lib/types';

interface Option {
  value: string;
  name: string;
}

async function readJSON<T>(response: Response): Promise<T> {
  const body = (await response.json().catch(() => ({}))) as T & { error?: string };
  if (!response.ok) throw new Error(body?.error || `HTTP ${response.status}`);
  return body;
}

interface RoomPickerProps {
  defaults: { feeitemid: number; appId: number };
  guestMode: boolean;
  onAdded: (room: RoomRef, targets: Target[] | null) => void;
  onStatus?: (message: string) => void;
}

/** 校区/楼栋/房间级联选择 + 添加宿舍，供「查询设置」与隐藏主页落地页共用。 */
export function RoomPicker({ defaults, guestMode, onAdded, onStatus }: RoomPickerProps) {
  const { adminFetch } = useAdmin();
  const [campuses, setCampuses] = React.useState<Option[]>([]);
  const [buildings, setBuildings] = React.useState<Option[]>([]);
  const [rooms, setRooms] = React.useState<Option[]>([]);
  const [campus, setCampus] = React.useState<Option | null>(null);
  const [building, setBuilding] = React.useState<Option | null>(null);
  const [room, setRoom] = React.useState<Option | null>(null);
  const [tag, setTag] = React.useState('');
  const [loadingCampuses, setLoadingCampuses] = React.useState(false);
  const [saving, setSaving] = React.useState(false);
  const [lookup, setLookup] = React.useState<{
    key: string;
    loading: boolean;
    exists: boolean;
    hidden: boolean;
    error: string | null;
  }>({ key: '', loading: false, exists: false, hidden: false, error: null });

  const campusSeq = React.useRef(0);
  const buildingSeq = React.useRef(0);
  const roomSeq = React.useRef(0);
  const lookupSeq = React.useRef(0);

  // onStatus 常为内联箭头函数；用 ref 固定引用，避免它进入 loadCampuses 的依赖
  // 导致每次渲染都重新加载校区（曾造成 /api/campuses 请求死循环）。
  const onStatusRef = React.useRef(onStatus);
  onStatusRef.current = onStatus;

  const selectedKey = campus && building && room
    ? `${campus.value}|${building.value}|${room.value}`
    : '';

  const loadCampuses = React.useCallback(async () => {
    const seq = ++campusSeq.current;
    buildingSeq.current++;
    roomSeq.current++;
    setLoadingCampuses(true);
    setCampuses([]);
    setBuildings([]);
    setRooms([]);
    setCampus(null);
    setBuilding(null);
    setRoom(null);
    setTag('');
    setLookup({ key: '', loading: false, exists: false, hidden: false, error: null });
    try {
      const list = await readJSON<Option[]>(await adminFetch('/api/campuses'));
      if (seq !== campusSeq.current) return;
      setCampuses(list);
      onStatusRef.current?.(list.length ? '' : '没有可用的校区');
    } catch (error) {
      if (seq !== campusSeq.current) return;
      onStatusRef.current?.('加载校区失败: ' + (error as Error).message);
    } finally {
      if (seq === campusSeq.current) setLoadingCampuses(false);
    }
  }, [adminFetch]);

  React.useEffect(() => {
    void loadCampuses();
  }, [loadCampuses]);

  React.useEffect(() => {
    if (!selectedKey) {
      lookupSeq.current++;
      setLookup({ key: '', loading: false, exists: false, hidden: false, error: null });
      return;
    }
    const seq = ++lookupSeq.current;
    setLookup({ key: selectedKey, loading: true, exists: false, hidden: false, error: null });
    const params = new URLSearchParams({
      campus: campus!.value,
      building: building!.value,
      room: room!.value,
    });
    adminFetch(`/api/config?${params.toString()}`)
      .then((response) => readJSON<{ target_exists?: boolean; target_hidden?: boolean }>(response))
      .then((result) => {
        if (seq !== lookupSeq.current) return;
        if (typeof result.target_exists !== 'boolean') throw new Error('宿舍查询结果不可用');
        setLookup({
          key: selectedKey,
          loading: false,
          exists: result.target_exists,
          hidden: result.target_exists && result.target_hidden === true,
          error: null,
        });
      })
      .catch((error) => {
        if (seq !== lookupSeq.current) return;
        setLookup({
          key: selectedKey,
          loading: false,
          exists: false,
          hidden: false,
          error: (error as Error).message,
        });
      });
  }, [selectedKey, campus, building, room, adminFetch]);

  async function onCampusChange(value: string) {
    const option = campuses.find((c) => c.value === value) || null;
    setCampus(option);
    setBuilding(null);
    setRoom(null);
    setBuildings([]);
    setRooms([]);
    setTag('');
    if (!option) return;
    const seq = ++buildingSeq.current;
    roomSeq.current++;
    try {
      const list = await readJSON<Option[]>(
        await adminFetch(`/api/buildings?${new URLSearchParams({ campus: option.value })}`),
      );
      if (seq === buildingSeq.current) setBuildings(list);
    } catch (error) {
      if (seq === buildingSeq.current) onStatusRef.current?.('加载楼栋失败: ' + (error as Error).message);
    }
  }

  async function onBuildingChange(value: string) {
    const option = buildings.find((b) => b.value === value) || null;
    setBuilding(option);
    setRoom(null);
    setRooms([]);
    setTag('');
    if (!option || !campus) return;
    const seq = ++roomSeq.current;
    try {
      const list = await readJSON<Option[]>(
        await adminFetch(
          `/api/rooms?${new URLSearchParams({ campus: campus.value, building: option.value })}`,
        ),
      );
      if (seq === roomSeq.current) setRooms(list);
    } catch (error) {
      if (seq === roomSeq.current) onStatusRef.current?.('加载房间失败: ' + (error as Error).message);
    }
  }

  function onRoomChange(value: string) {
    const option = rooms.find((r) => r.value === value) || null;
    setRoom(option);
    setTag(option ? option.name : '');
  }

  const canSubmit =
    !!campus && !!building && !!room && !lookup.loading && lookup.key === selectedKey && !lookup.error && !saving;

  async function addTarget() {
    if (!campus || !building || !room || !selectedKey) return;
    const target: Target = {
      feeitemid: defaults.feeitemid,
      appId: defaults.appId,
      campus: campus.value,
      building: building.value,
      room: room.value,
      label: tag.trim() || `${campus.name}/${building.name}/${room.name}`,
    };
    if (lookup.exists && !lookup.hidden) {
      // 宿舍已存在：直接进入该宿舍（携带 targets=null 表示调用方应导航）。
      onAdded(
        { campus: target.campus, building: target.building, room: target.room, label: target.label },
        null,
      );
      return;
    }
    if (lookup.exists && lookup.hidden) target.show_in_web = true;
    setSaving(true);
    try {
      const body = await readJSON<{ targets: Target[] | null }>(
        await adminFetch('/api/config', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ target }),
        }),
      );
      const added = {
        campus: target.campus,
        building: target.building,
        room: target.room,
        label: target.label,
      };
      // 解除隐藏、访客添加：直接进入该宿舍；只有持密钥的管理员原地保存并留在弹窗。
      if ((lookup.exists && lookup.hidden) || guestMode) {
        onAdded(added, null);
        return;
      }
      onAdded(added, body.targets);
      onStatusRef.current?.('已添加并自动保存');
      setBuildings([]);
      setRooms([]);
      setBuilding(null);
      setRoom(null);
      setTag('');
    } catch (error) {
      onStatusRef.current?.('保存失败: ' + (error as Error).message);
    } finally {
      setSaving(false);
    }
  }

  const preview = (() => {
    if (!campus || !building || !room) return '';
    if (lookup.loading || lookup.key !== selectedKey) return '正在查询宿舍…';
    if (lookup.error) return '查询失败: ' + lookup.error;
    if (lookup.exists) return lookup.hidden ? '宿舍已存在（进入后自动解除隐藏）' : '宿舍已存在';
    return `将添加: ${campus.name}/${building.name}/${room.name}`;
  })();

  return (
    <section>
      <h3 className="mb-2 text-xs font-semibold text-muted-foreground">添加宿舍(电费)</h3>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-[1fr_1fr_1fr_auto]">
        <Select
          aria-label="选择校区"
          value={campus?.value ?? ''}
          disabled={loadingCampuses}
          onChange={(event) => void onCampusChange(event.target.value)}
        >
          <option value="">{loadingCampuses ? '— 加载校区 —' : '— 选择校区 —'}</option>
          {campuses.map((c) => (
            <option key={c.value} value={c.value}>
              {c.name}
            </option>
          ))}
        </Select>
        <Select
          aria-label="选择楼栋"
          value={building?.value ?? ''}
          disabled={!campus || buildings.length === 0}
          onChange={(event) => void onBuildingChange(event.target.value)}
        >
          <option value="">— 选择楼栋 —</option>
          {buildings.map((b) => (
            <option key={b.value} value={b.value}>
              {b.name}
            </option>
          ))}
        </Select>
        <Select
          aria-label="选择房间"
          value={room?.value ?? ''}
          disabled={!building || rooms.length === 0}
          onChange={(event) => onRoomChange(event.target.value)}
        >
          <option value="">— 选择房间 —</option>
          {rooms.map((r) => (
            <option key={r.value} value={r.value}>
              {r.name}
            </option>
          ))}
        </Select>
        <Button onClick={() => void addTarget()} disabled={!canSubmit}>
          {lookup.exists && !lookup.hidden ? '查看宿舍' : '添加'}
        </Button>
      </div>
      <div className="mt-3 flex items-center gap-2">
        <Label htmlFor="pick-tag" className="shrink-0">
          宿舍 tag / 别名
        </Label>
        <Input
          id="pick-tag"
          value={tag}
          maxLength={40}
          placeholder="例如：我的宿舍"
          onChange={(event) => setTag(event.target.value)}
        />
      </div>
      <p className="mt-2 min-h-[18px] text-xs text-muted-foreground">{preview}</p>
    </section>
  );
}
