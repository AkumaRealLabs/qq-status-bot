import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCcw, Save } from 'lucide-react'
import { EmptyPanel, Field, FormError, StatusBadge } from '@/components/common'
import { Page, ShellLoading } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ModelCard, SchedulerChannel, SchedulerConfig } from '@/types'

const none = '__none__'

export function SchedulerPage() {
  const qc = useQueryClient()
  const [cfgDraft, setCfgDraft] = useState<SchedulerConfig | null>(null)
  const [keyword, setKeyword] = useState('')
  const [message, setMessage] = useState('')
  const cfg = useQuery({ queryKey: ['scheduler', 'config'], queryFn: () => api<SchedulerConfig>('/api/scheduler/config') })
  const cards = useQuery({ queryKey: ['cards'], queryFn: () => api<ModelCard[]>('/api/cards') })
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
          {(cfg.isFetching || cards.isFetching || channels.isFetching) && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <Button variant="outline" size="sm" onClick={() => void channels.refetch()} disabled={channels.isFetching}>
            <RefreshCcw className={cn('size-4', channels.isFetching && 'animate-spin')} />
            刷新渠道
          </Button>
        </div>
      }
    >
      <Card className="w-full max-w-3xl bg-card">
        <CardHeader>
          <CardTitle>连接配置</CardTitle>
          <CardDescription>保存后刷新渠道列表</CardDescription>
        </CardHeader>
        <CardContent className="grid min-w-0 gap-4 md:grid-cols-2">
          <Field label="Base URL">
            <Input value={form.scheduler_base_url} onChange={(e) => setCfgDraft({ ...form, scheduler_base_url: e.target.value })} />
          </Field>
          <Field label="用户 ID">
            <Input value={form.scheduler_user_id} onChange={(e) => setCfgDraft({ ...form, scheduler_user_id: e.target.value })} />
          </Field>
          <Field label="Access Token">
            <Input type="password" value={form.scheduler_access_token} onChange={(e) => setCfgDraft({ ...form, scheduler_access_token: e.target.value })} />
          </Field>
          <Field label="渠道搜索">
            <Input value={keyword} placeholder="名称、模型或分组" onChange={(e) => setKeyword(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') void channels.refetch() }} />
          </Field>
          <div className="flex flex-wrap items-center gap-2 md:col-span-2">
            <Button onClick={() => saveConfig.mutate()} disabled={saveConfig.isPending}>
              {saveConfig.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
              保存配置
            </Button>
            <Button variant="outline" onClick={() => void channels.refetch()} disabled={channels.isFetching}>
              测试连接
            </Button>
            {message && <span className={cn('text-sm', saveConfig.isError ? 'text-destructive' : 'text-muted-foreground')}>{message}</span>}
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
                      {channelsForCard(list, card).map((channel) => (
                        <SelectItem key={channel.id} value={channel.id}>
                          {channel.name || channel.id}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <div className="text-xs leading-relaxed text-muted-foreground">
                  {channelDetail(channelsForCard(list, card).find((item) => item.id === card.scheduler_channel_id))}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </Page>
  )
}

function channelDetail(channel?: SchedulerChannel) {
  if (!channel) return '先刷新渠道，再选择要绑定的渠道'
  return [`状态 ${channel.status}`, channel.type, channel.group, channel.tag, channel.models?.join(', ')].filter(Boolean).join(' · ')
}

function channelsForCard(channels: SchedulerChannel[], card: ModelCard) {
  if (!card.scheduler_channel_id || channels.some((item) => item.id === card.scheduler_channel_id)) return channels
  return [{ id: card.scheduler_channel_id, name: card.scheduler_channel_name || card.scheduler_channel_id, status: 0 }, ...channels]
}
