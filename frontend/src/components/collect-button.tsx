import * as React from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { IconBolt } from '@tabler/icons-react';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/toast';
import { useAdmin } from '@/lib/admin';
import { useApp } from '@/lib/app-context';
import { fmt } from '@/lib/format';
import type { CollectStatus } from '@/lib/types';

const ACTIVE_JOB_KEY = 'elec-active-collect-job';

function readActiveJob(): string {
  try {
    return sessionStorage.getItem(ACTIVE_JOB_KEY) || '';
  } catch {
    return '';
  }
}

function writeActiveJob(jobId: string): void {
  try {
    if (jobId) sessionStorage.setItem(ACTIVE_JOB_KEY, jobId);
    else sessionStorage.removeItem(ACTIVE_JOB_KEY);
  } catch {
    /* ignore */
  }
}

const TERMINAL = new Set(['done', 'failed', 'cancelled']);

export function CollectButton() {
  const { config, room, targets } = useApp();
  const { adminFetch, ensureAdmin } = useAdmin();
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const [job, setJob] = React.useState<CollectStatus | null>(null);
  const [activeJobId, setActiveJobId] = React.useState(readActiveJob);
  const [busy, setBusy] = React.useState(false);
  const mounted = React.useRef(true);
  React.useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const refresh = React.useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ['readings'] });
    void queryClient.invalidateQueries({ queryKey: ['daily'] });
    void queryClient.invalidateQueries({ queryKey: ['recharges'] });
    void queryClient.invalidateQueries({ queryKey: ['power'] });
  }, [queryClient]);

  const poll = React.useCallback(
    async (jobId: string): Promise<CollectStatus | null> => {
      for (;;) {
        if (!mounted.current) return null;
        const response = await adminFetch(
          `/api/collect-all/status?job_id=${encodeURIComponent(jobId)}`,
        );
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const status = (await response.json()) as CollectStatus;
        if (!mounted.current) return null;
        setJob(status);
        if (TERMINAL.has(status.state)) return status;
        await new Promise((resolve) => setTimeout(resolve, 800));
      }
    },
    [adminFetch],
  );

  const finishJob = React.useCallback(
    (status: CollectStatus | null) => {
      if (!status) return;
      if (status.state === 'cancelled') {
        toast(`采集已取消，已完成 ${status.completed || 0}/${status.requested || 0}`);
      } else if (status.state === 'failed') {
        toast('批量采集失败: ' + (status.error || '未知错误'), true);
      } else {
        const failed = status.failed || (status.results || []).filter((r) => r.error).length;
        const all = !failed && status.success === status.requested;
        const parts = [`成功 ${status.success || 0}/${status.requested || 0}`];
        if (failed) parts.push(`${failed} 间失败`);
        toast(all ? `已采集全部 ${status.success} 间` : '采集结果: ' + parts.join(','), !all);
      }
      refresh();
    },
    [toast, refresh],
  );

  const startCollectAll = React.useCallback(async () => {
    const response = await adminFetch('/api/collect-all', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    if (!response.ok) {
      const body = (await response.json().catch(() => ({}))) as { error?: string };
      throw new Error(body.error || `HTTP ${response.status}`);
    }
    const started = (await response.json()) as CollectStatus;
    setActiveJobId(started.job_id);
    writeActiveJob(started.job_id);
    const finalStatus = await poll(started.job_id);
    setActiveJobId('');
    writeActiveJob('');
    finishJob(finalStatus);
  }, [adminFetch, poll, finishJob]);

  const startCollectRoom = React.useCallback(
    async (campus: string, building: string, roomId: string) => {
      const response = await adminFetch('/api/collect', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ campus, building, room: roomId }),
      });
      const body = (await response.json().catch(() => ({}))) as {
        ok?: boolean;
        error?: string;
        display_label?: string;
        room_label?: string;
        surplus_charge?: number | null;
      };
      if (body.ok) {
        toast(`已采集 ${body.display_label || body.room_label}:剩余 ${fmt(body.surplus_charge)} kWh`);
      } else {
        toast('采集失败:' + (body.error || `HTTP ${response.status}`), true);
      }
      refresh();
    },
    [adminFetch, toast, refresh],
  );

  const handleCollect = React.useCallback(async () => {
    setBusy(true);
    try {
      if (!(await ensureAdmin(config?.admin_auth_required === true))) return;
      if (room) {
        const matched = targets.find(
          (t) => t.campus === room.campus && t.building === room.building && t.room === room.room,
        );
        if (!matched) {
          toast('该宿舍不在监控列表中,无法立即采集。可在「查询设置」里添加', true);
          return;
        }
        if (!matched.campus || !matched.building || !matched.room) return;
        await startCollectRoom(matched.campus, matched.building, matched.room);
        return;
      }
      if (!targets.length) {
        toast('还没有监控宿舍,先到「查询设置」添加', true);
        return;
      }
      await startCollectAll();
    } catch (error) {
      toast('采集失败: ' + (error as Error).message, true);
    } finally {
      if (mounted.current) setBusy(false);
    }
  }, [ensureAdmin, config, room, targets, toast, startCollectRoom, startCollectAll]);

  const handleCancel = React.useCallback(async () => {
    if (!activeJobId) return;
    try {
      if (!(await ensureAdmin(config?.admin_auth_required === true))) return;
      const response = await adminFetch(
        `/api/collect-all/cancel?job_id=${encodeURIComponent(activeJobId)}`,
        { method: 'POST' },
      );
      if (response.ok) {
        toast('取消请求已发送，正在等待当前请求退出');
        setJob((prev) => (prev ? { ...prev, state: 'cancelling' } : prev));
      }
    } catch (error) {
      toast('取消失败: ' + (error as Error).message, true);
    }
  }, [activeJobId, ensureAdmin, config, adminFetch, toast]);

  // 刷新页面后恢复未完成的批量任务。
  React.useEffect(() => {
    const jobId = readActiveJob();
    if (!jobId) return;
    let active = true;
    (async () => {
      try {
        const status = await poll(jobId);
        if (!active) return;
        setActiveJobId('');
        writeActiveJob('');
        finishJob(status);
      } catch (error) {
        if (active) writeActiveJob('');
        console.warn('恢复批量任务失败:', error);
      }
    })();
    return () => {
      active = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const running = !!job && !TERMINAL.has(job.state);
  const cancelling = job?.state === 'cancelling';
  const total = job?.requested || 0;
  const done = job?.completed || 0;
  const progress = running
    ? Math.max(0, Math.min(100, Number(job?.percent ?? (total ? (done * 100) / total : 0))))
    : 0;
  const label = cancelling
    ? '正在取消…'
    : running
      ? total
        ? `采集 ${done}/${total}`
        : '准备采集…'
      : '立即采集';

  return (
    <div className="flex items-center gap-2">
      <Button
        onClick={handleCollect}
        disabled={busy || cancelling}
        title={running && job?.current?.label ? `正在采集: ${job.current.label}` : undefined}
        className="relative overflow-hidden"
      >
        {running && (
          <span
            aria-hidden="true"
            className="absolute inset-y-0 left-0 bg-white/25 transition-[width] duration-200"
            style={{ width: `${progress}%` }}
          />
        )}
        <span className="relative inline-flex items-center gap-1.5">
          {!running && <IconBolt className="size-4" />}
          {label}
        </span>
      </Button>
      {running && (
        <Button variant="outline" size="sm" onClick={handleCancel} disabled={cancelling}>
          取消采集
        </Button>
      )}
    </div>
  );
}
