import * as React from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { RoomPicker } from '@/components/room-picker';
import { useToast } from '@/components/toast';
import { useAdmin } from '@/lib/admin';
import { useApp } from '@/lib/app-context';
import { roomPath } from '@/lib/constants';

/** 主页隐藏且未登录时的落地页：选择校区/楼栋/房间进入宿舍，或登录查看全部。 */
export function HomePicker() {
  const { config, canAdd } = useApp();
  const { ensureKey } = useAdmin();
  const { toast } = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [status, setStatus] = React.useState('');

  async function login() {
    try {
      if (!(await ensureKey())) return;
      await queryClient.invalidateQueries({ queryKey: ['config'] });
      await queryClient.invalidateQueries({ queryKey: ['readings'] });
    } catch (error) {
      toast('登录失败: ' + (error as Error).message, true);
    }
  }

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle>主页已隐藏</CardTitle>
        <span className="text-xs text-muted-foreground">
          {canAdd ? '选择校区/楼栋/房间即可进入或添加宿舍' : '登录后可添加宿舍并查看全部宿舍数据'}
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
        {!canAdd && <p className="text-xs text-muted-foreground">登录后即可添加宿舍</p>}
        {canAdd && status && <p className="min-h-[18px] text-xs text-muted-foreground">{status}</p>}
        <div className="flex justify-end">
          <Button onClick={() => void login()}>登录查看全部宿舍</Button>
        </div>
      </CardContent>
    </Card>
  );
}
