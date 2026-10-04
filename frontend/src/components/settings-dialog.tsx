import * as React from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Dialog, DialogBody, DialogClose, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { RoomPicker } from '@/components/room-picker';
import { useToast } from '@/components/toast';
import { useAdmin } from '@/lib/admin';
import { useApp } from '@/lib/app-context';
import { roomPath, targetLabel } from '@/lib/constants';
import type { RoomRef, Target } from '@/lib/types';
import { cn } from '@/lib/utils';

interface SettingsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

async function readJSON<T>(response: Response): Promise<T> {
  const body = (await response.json().catch(() => ({}))) as T & { error?: string };
  if (!response.ok) throw new Error(body?.error || `HTTP ${response.status}`);
  return body;
}

export function SettingsDialog({ open, onOpenChange }: SettingsDialogProps) {
  const { config, canAdd, room } = useApp();
  const { adminFetch, ensureKey, key, verified } = useAdmin();
  const { toast } = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [draftTargets, setDraftTargets] = React.useState<Target[]>([]);
  const [showTargets, setShowTargets] = React.useState(true);
  const [message, setMessage] = React.useState('');

  React.useEffect(() => {
    if (!open) return;
    let active = true;
    (async () => {
      const allowed = canAdd || (await ensureKey());
      if (!active || !allowed) return;
      const visible = config?.show_homepage !== false || !!key || verified;
      setShowTargets(visible);
      if (!visible) {
        setDraftTargets([]);
        return;
      }
      try {
        const cfg = await readJSON<{ targets: Target[] | null }>(
          await adminFetch('/api/config?admin=1'),
        );
        if (active) setDraftTargets((cfg.targets || []).map((t) => ({ ...t })));
      } catch (error) {
        if (active) toast('加载监控宿舍失败: ' + (error as Error).message, true);
      }
    })();
    return () => {
      active = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const handleAdded = (added: RoomRef, targets: Target[] | null) => {
    void queryClient.invalidateQueries({ queryKey: ['config'] });
    if (targets) {
      setDraftTargets(targets.map((t) => ({ ...t })));
      return;
    }
    onOpenChange(false);
    void queryClient.invalidateQueries({ queryKey: ['readings'] });
    void navigate({ to: roomPath(added) });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange} className="max-w-2xl">
      <DialogHeader>
        <DialogTitle>查询设置</DialogTitle>
        <DialogClose onClick={() => onOpenChange(false)} />
      </DialogHeader>
      <DialogBody className="space-y-5">
        {showTargets && (
          <section>
            <h3 className="mb-2 text-xs font-semibold text-muted-foreground">已监控宿舍</h3>
            <div className="flex flex-col gap-2">
              {draftTargets.length === 0 && (
                <p className="text-xs text-muted-foreground">还没有监控宿舍,在下面添加。</p>
              )}
              {draftTargets.map((t) => (
                <div
                  key={`${t.campus}|${t.building}|${t.room}`}
                  className={cn(
                    'flex items-center gap-2 rounded-md border border-border bg-muted px-3 py-2 text-sm',
                    room &&
                      t.campus === room.campus &&
                      t.building === room.building &&
                      t.room === room.room &&
                      'border-ring/60',
                  )}
                >
                  <span className="min-w-0 flex-1 truncate">{targetLabel(t)}</span>
                  <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                    {t.campus}/{t.building}/{t.room}
                  </span>
                </div>
              ))}
            </div>
          </section>
        )}

        {config && (
          <RoomPicker
            defaults={config.defaults}
            guestMode={!key && !verified}
            onAdded={handleAdded}
            onStatus={setMessage}
          />
        )}
        {message && <p className="text-xs text-muted-foreground">{message}</p>}
      </DialogBody>
    </Dialog>
  );
}
