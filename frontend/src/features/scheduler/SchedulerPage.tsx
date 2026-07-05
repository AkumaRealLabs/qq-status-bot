import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCcw, Save, Settings2 } from 'lucide-react'
import { EmptyPanel, Field, FormError, StatusBadge } from '@/components/common'
import { Page, ShellLoading } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage, fmtTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ModelCard, SchedulerChannel, SchedulerConfig, SchedulerLog } from '@/types'

const none = '__none__'

export function SchedulerPage() {
  const qc = useQueryClient()
  const [cfgDraft, setCfgDraft] = useState<SchedulerConfig | null>(null)
  const [keyword, setKeyword] = useState('')
  const [message, setMessage] = useState('')
  const cfg = useQuery({ queryKey: ['scheduler', 'config'], queryFn: () => api<SchedulerConfig>('/api/scheduler/config') })
  const cards = useQuery({ queryKey: ['cards'], queryFn: () => api<ModelCard[]>('/api/cards') })
  const logs = useQuery({ queryKey: ['scheduler', 'logs'], queryFn: () => api<SchedulerLog[]>('/api/scheduler/logs?limit=50') })
  const channels = useQuery({
    queryKey: ['scheduler', 'channels', keyword],
    queryFn: () => api<SchedulerChannel[]>(`/api/scheduler/channels?keyword=${encodeURIComponent(keyword)}`),
    enabled: false,
  })
  const form = cfgDraft ?? cfg.data
  const saveConfig = useMutation({
    mutationFn: () => api<SchedulerConfig>('/api/scheduler/config', { method: 'PATCH', body: JSON.stringify(form) }),
    onSuccess: async (data) => {
      setMessage('已保存')
      setCfgDraft(null)
      await qc.setQueryData(['scheduler', 'config'], data)
      void channels.refetch()
    },
    onError: (error) => setMessage(errorMessage(error)),
  })
  const bind = useMutation({
    mutationFn: ({ card, channel }: { card: ModelCard; channel?: SchedulerChannel }) =>
      api(`/api/cards/${card.id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          scheduler_channel_id: channel?.id ?? '',
          scheduler_channel_name: channel?.name ?? '',
        }),
      }),
    onSuccess: async () => {
      await Promise.all([qc.invalidateQueries({ queryKey: ['cards'] }), qc.invalidateQueries({ queryKey: ['status'] })])
    },
    onError: (error) => window.alert(errorMessage(error)),
  })
  if (!form) return <ShellLoading />
  const rows = cards.data ?? []
  const list = channels.data ?? []
  return (
    <Page
      title="调度器"
      description="连接 new-api 调度器并绑定状态卡片"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {(cfg.isFetching || cards.isFetching || channels.isFetching || logs.isFetching) && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <SchedulerConfigDialog
            form={form}
            message={message}
            saveError={saveConfig.isError}
            saving={saveConfig.isPending}
            onChange={(patch) => setCfgDraft({ ...form, ...patch })}
            onSave={() => saveConfig.mutate()}
          />
          <Button variant="outline" size="sm" onClick={() => void channels.refetch()} disabled={channels.isFetching}>
            <RefreshCcw className={cn('size-4', channels.isFetching && 'animate-spin')} />
            刷新渠道
          </Button>
        </div>
      }
    >
      <Card className="w-full max-w-3xl bg-card">
        <CardContent className="grid min-w-0 gap-4 md:grid-cols-[1fr_auto]">
          <Field label="渠道搜索">
            <Input value={keyword} placeholder="名称、模型或分组" onChange={(e) => setKeyword(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') void channels.refetch() }} />
          </Field>
          <div className="flex items-end">
            <Button variant="outline" onClick={() => void channels.refetch()} disabled={channels.isFetching}>
              {channels.isFetching ? <Loader2 className="size-4 animate-spin" /> : <RefreshCcw className="size-4" />}
              测试连接
            </Button>
          </div>
          <div className="md:col-span-2">
            <FormError error={channels.error} />
          </div>
        </CardContent>
      </Card>

      {cards.isLoading && <EmptyPanel text="加载中..." />}
      {!cards.isLoading && rows.length === 0 && <EmptyPanel text="暂无状态卡片" />}
      {!cards.isLoading && rows.length > 0 && (
        <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((card) => (
            <Card key={card.id} className="min-w-0 bg-card">
              <CardHeader className="gap-2">
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0">
                    <CardTitle className="truncate">{card.name}</CardTitle>
                    <CardDescription>{card.scheduler_channel_name || '未绑定渠道'}</CardDescription>
                  </div>
                  <StatusBadge ok={!card.scheduler_auto_disabled} okText={card.scheduler_channel_id ? '可自动控制' : '未绑定'} failText="已自动关闭" />
                </div>
              </CardHeader>
              <CardContent className="grid gap-3">
                <Field label="调度器渠道">
                  <Select
                    value={card.scheduler_channel_id || none}
                    onValueChange={(value) => bind.mutate({ card, channel: value === none ? undefined : list.find((item) => item.id === value) })}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="选择渠道" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={none}>不绑定</SelectItem>
                      {channelsForCard(list, card, rows).map((channel) => (
                        <SelectItem key={channel.id} value={channel.id}>
                          {channel.name || channel.id}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <div className="text-xs leading-relaxed text-muted-foreground">
                  {channelDetail(channelsForCard(list, card, rows).find((item) => item.id === card.scheduler_channel_id))}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Card className="min-w-0 bg-card">
        <CardHeader>
          <div className="flex items-center justify-between gap-3">
            <div>
              <CardTitle>调度日志</CardTitle>
              <CardDescription>自动关闭和恢复渠道的执行记录</CardDescription>
            </div>
            <Button variant="outline" size="sm" onClick={() => void logs.refetch()} disabled={logs.isFetching}>
              <RefreshCcw className={cn('size-4', logs.isFetching && 'animate-spin')} />
              刷新
            </Button>
          </div>
        </CardHeader>
        <CardContent className="grid gap-2">
          {logs.isLoading && <EmptyPanel text="加载中..." />}
          <FormError error={logs.error} />
          {!logs.isLoading && !logs.error && (logs.data ?? []).length === 0 && <EmptyPanel text="暂无调度日志" />}
          {(logs.data ?? []).map((log) => (
            <div key={log.id} className="grid min-w-0 gap-1 rounded-md border border-border bg-muted/20 px-3 py-2 text-sm md:grid-cols-[140px_1fr_auto] md:items-center">
              <span className="text-xs text-muted-foreground">{fmtTime(log.created_at)}</span>
              <div className="min-w-0">
                <div className="truncate font-medium">{log.card_name || log.card_id || '未知卡片'} · {log.channel_name || log.channel_id || '未知渠道'}</div>
                <div className="truncate text-xs text-muted-foreground">{log.message}</div>
              </div>
              <span className={cn('text-xs font-medium', log.status === 'success' ? 'text-emerald-600' : log.status === 'error' ? 'text-destructive' : 'text-muted-foreground')}>
                {logAction(log.action)} · {logStatus(log.status)}
              </span>
            </div>
          ))}
        </CardContent>
      </Card>
    </Page>
  )
}

function SchedulerConfigDialog({
  form,
  message,
  saveError,
  saving,
  onChange,
  onSave,
}: {
  form: SchedulerConfig
  message: string
  saveError: boolean
  saving: boolean
  onChange: (patch: Partial<SchedulerConfig>) => void
  onSave: () => void
}) {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <Settings2 className="size-4" />
          连接配置
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>连接配置</DialogTitle>
        <div className="grid min-w-0 gap-4 md:grid-cols-2">
          <Field label="Base URL">
            <Input value={form.scheduler_base_url} onChange={(e) => onChange({ scheduler_base_url: e.target.value })} />
          </Field>
          <Field label="用户 ID">
            <Input value={form.scheduler_user_id} onChange={(e) => onChange({ scheduler_user_id: e.target.value })} />
          </Field>
          <Field label="Access Token">
            <Input type="password" value={form.scheduler_access_token} onChange={(e) => onChange({ scheduler_access_token: e.target.value })} />
          </Field>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {message && <span className={cn('text-sm', saveError ? 'text-destructive' : 'text-muted-foreground')}>{message}</span>}
          <Button onClick={onSave} disabled={saving}>
            {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            保存配置
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function channelDetail(channel?: SchedulerChannel) {
  if (!channel) return '先刷新渠道，再选择要绑定的渠道'
  return [`状态 ${channel.status}`, channel.type, channel.group, channel.tag, channel.models?.join(', ')].filter(Boolean).join(' · ')
}

function channelsForCard(channels: SchedulerChannel[], card: ModelCard, cards: ModelCard[]) {
  const used = new Set(cards.filter((item) => item.id !== card.id).map((item) => item.scheduler_channel_id).filter(Boolean))
  const available = channels.filter((channel) => !used.has(channel.id))
  if (!card.scheduler_channel_id || available.some((item) => item.id === card.scheduler_channel_id)) return available
  return [{ id: card.scheduler_channel_id, name: card.scheduler_channel_name || card.scheduler_channel_id, status: 0 }, ...available]
}

function logAction(action: SchedulerLog['action']) {
  return action === 'restore' ? '恢复' : '关闭'
}

function logStatus(status: SchedulerLog['status']) {
  if (status === 'success') return '成功'
  if (status === 'error') return '失败'
  return '跳过'
}
