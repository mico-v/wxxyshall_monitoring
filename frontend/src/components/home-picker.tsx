import * as React from 'react';
import { useNavigate } from '@tanstack/react-router';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { RoomPicker } from '@/components/room-picker';
import { useApp } from '@/lib/app-context';
import { roomPath } from '@/lib/constants';

/** 聚合主页不可见时的落地页：访客仍可选择或添加宿舍。 */
export function HomePicker() {
  const { config, canAdd } = useApp();
  const navigate = useNavigate();
  const [status, setStatus] = React.useState('');

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle>{canAdd ? '选择宿舍' : '主页已隐藏'}</CardTitle>
        <span className="text-xs text-muted-foreground">
          {canAdd ? '进入已添加宿舍，或选择新宿舍' : '请通过“查询设置”登录'}
        </span>
      </CardHeader>
      <CardContent className="space-y-4">
        {config && canAdd && (
          <RoomPicker
            defaults={config.defaults}
            guestMode
            onAdded={(room) => void navigate({ to: roomPath(room) })}
            onStatus={setStatus}
          />
        )}
        {!canAdd && <p className="text-xs text-muted-foreground">需要管理密钥</p>}
        {canAdd && status && <p className="min-h-[18px] text-xs text-muted-foreground">{status}</p>}
      </CardContent>
    </Card>
  );
}
