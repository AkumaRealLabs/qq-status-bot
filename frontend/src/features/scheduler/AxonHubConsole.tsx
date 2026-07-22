import { useEffect, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Database, Loader2, RefreshCcw, RotateCcw, ShieldAlert, SlidersHorizontal, Wifi } from 'lucide-react'
import { DataTable, EmptyPanel, Field, FormError, InlineMessage, Metric } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { fmtTime } from '@/lib/format'
import { alertError, secretPlaceholder } from '@/lib/feedback'
import { cn } from '@/lib/utils'
import type { AxonHubConfig, AxonHubControlPlane, AxonHubPreflight, ModelCard, SchedulerChannel, SchedulerLog } from '@/types'

const none = '__none__'

export function AxonHubConsole() {
  const qc = useQueryClient()
  const [draft, setDraft] = useState<AxonHubConfig>()
  const config = useQuery({ queryKey: ['scheduler', 'axonhub', 'config'], queryFn: () => api<AxonHubConfig>('/api/scheduler/axonhub/config') })
  const cards = useQuery({ queryKey: ['cards'], queryFn: () => api<ModelCard[]>('/api/cards') })
  const configured = Boolean(config.data?.base_url && config.data?.admin_email && config.data.admin_password_set)
  const channels = useQuery({ queryKey: ['scheduler', 'channels'], queryFn: () => api<SchedulerChannel[]>('/api/scheduler/channels'), enabled: configured })
  const control = useQuery({
    queryKey: ['scheduler', 'axonhub', 'control-plane'],
    queryFn: () => api<AxonHubControlPlane>('/api/scheduler/control-plane'),
    enabled: configured,
    refetchInterval: 10000,
  })
  const preflight = useQuery({
    queryKey: ['scheduler', 'axonhub', 'preflight'],
    queryFn: () => api<AxonHubPreflight>('/api/scheduler/axonhub/preflight'),
    enabled: false,
  })
  useEffect(() => { if (config.data) setDraft(config.data) }, [config.data])
  const refresh = () => {
    void Promise.all([config.refetch(), cards.refetch(), channels.refetch(), control.refetch()])
  }
  const saveConfig = useMutation({
    mutationFn: () => api<AxonHubConfig>('/api/scheduler/axonhub/config', { method: 'PATCH', body: JSON.stringify(draft) }),
    onSuccess: async (next) => {
      setDraft(next)
      await qc.setQueryData(['scheduler', 'axonhub', 'config'], next)
      refresh()
    },
    onError: alertError,
  })
  const test = useMutation({ mutationFn: () => api('/api/scheduler/axonhub/test', { method: 'POST' }), onError: alertError })
  const adopt = useMutation({
    mutationFn: (channelID: string) => api(`/api/scheduler/control-plane/${channelID}/adopt`, { method: 'POST' }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['scheduler'] }) },
    onError: alertError,
  })
  const adoptBound = useMutation({
    mutationFn: () => api('/api/scheduler/control-plane/adopt-bound', { method: 'POST' }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['scheduler'] }) },
    onError: alertError,
  })
  const switchProvider = useMutation({
    mutationFn: () => api('/api/scheduler/provider/switch', { method: 'POST', body: JSON.stringify({ provider: 'ggapi' }) }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['scheduler'] }) },
    onError: alertError,
  })
  const bind = useMutation({
    mutationFn: ({ card, channel }: { card: ModelCard; channel?: SchedulerChannel }) => api(`/api/cards/${card.id}`, {
      method: 'PATCH', body: JSON.stringify({ axonhub_channel_id: channel?.id ?? '', axonhub_channel_name: channel?.name ?? '' }),
    }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['cards'] }); void qc.invalidateQueries({ queryKey: ['scheduler'] }) },
    onError: alertError,
  })
  const rows = control.data?.channels ?? []
  const allCards = cards.data ?? []
  const poolCards = allCards.filter((card) => card.pool_enabled)
  const external = rows.filter((row) => row.external_takeover).length
  const disabled = rows.filter((row) => row.remote_status === 'disabled').length
  const active = config.data?.control_mode === 'active'
  return (
    <section className="grid min-w-0 gap-5">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <h2 className="text-lg font-semibold">AxonHub 控制台</h2>
          <Badge variant={active ? 'success' : config.data?.control_mode === 'off' ? 'outline' : 'secondary'}>{controlModeText(config.data?.control_mode)}</Badge>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void preflight.refetch()} disabled={!configured || preflight.isFetching}>
            {preflight.isFetching ? <Loader2 className="size-4 animate-spin" /> : <CheckCircle2 className="size-4" />}预检
          </Button>
          <Button variant="outline" size="sm" onClick={() => test.mutate()} disabled={!configured || test.isPending}>
            {test.isPending ? <Loader2 className="size-4 animate-spin" /> : <Wifi className="size-4" />}测试
          </Button>
          <AxonHubConfigDialog draft={draft} onChange={setDraft} onSave={() => saveConfig.mutate()} saving={saveConfig.isPending} />
          <Button variant="outline" size="icon" title="刷新" onClick={refresh} disabled={control.isFetching}>
            <RefreshCcw className={cn('size-4', control.isFetching && 'animate-spin')} /><span className="sr-only">刷新</span>
          </Button>
          <Button variant="outline" size="sm" onClick={() => switchProvider.mutate()} disabled={switchProvider.isPending}>切换 GGAPI</Button>
        </div>
      </div>
      {!configured && <InlineMessage message="请配置 AxonHub 连接" tone="warning" />}
      {preflight.data && <PreflightPanel value={preflight.data} />}
      {configured && <>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <Metric label="绑定渠道" value={rows.length} />
          <Metric label="AUM 已关闭" value={rows.filter((row) => row.aum_disabled).length} accent={rows.some((row) => row.aum_disabled) ? 'danger' : undefined} />
          <Metric label="外部接管" value={external} accent={external ? 'danger' : undefined} />
          <Metric label="远端关闭" value={disabled} accent={disabled ? 'danger' : undefined} />
        </div>
        <FormError error={channels.error || control.error} />
        <section className="grid min-w-0 gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-base font-semibold">卡片绑定</h3><Button variant="outline" size="sm" onClick={() => adoptBound.mutate()} disabled={adoptBound.isPending}>{adoptBound.isPending ? <Loader2 className="size-4 animate-spin" /> : <ShieldAlert className="size-4" />}接管已绑定渠道</Button></div>
          {poolCards.length === 0 ? <EmptyPanel text="暂无号池卡片" /> : <DataTable minWidthClass="min-w-[860px]" head={<tr><Header>卡片</Header><Header>GPT 模型</Header><Header>成本</Header><Header>AxonHub 渠道</Header><Header>状态</Header></tr>}>
            {poolCards.map((card) => {
              const options = axonHubOptions(channels.data ?? [], card, poolCards)
              const current = options.find((item) => item.id === card.axonhub_channel_id)
              const state = rows.find((item) => item.card_id === card.id)
              return <tr key={card.id} className="border-t border-border"><Cell><div className="font-medium">{card.name}</div></Cell><Cell>{card.model}</Cell><Cell>{card.effective_ratio || card.manual_cost_ratio || '-'}</Cell><Cell><Select value={card.axonhub_channel_id || none} onValueChange={(value) => bind.mutate({ card, channel: value === none ? undefined : options.find((item) => item.id === value) })}><SelectTrigger className="min-w-64"><SelectValue placeholder="选择渠道" /></SelectTrigger><SelectContent><SelectItem value={none}>不绑定</SelectItem>{options.map((item) => <SelectItem key={item.id} value={item.id}>{item.name || item.id}</SelectItem>)}</SelectContent></Select></Cell><Cell>{current ? <Badge variant={current.remote_status === 'enabled' ? 'success' : 'secondary'}>{channelStatus(current.remote_status)}</Badge> : state?.external_takeover ? <Badge variant="destructive">外部接管</Badge> : '-'}</Cell></tr>
            })}
          </DataTable>}
        </section>
        <section className="grid min-w-0 gap-3">
          <h3 className="text-base font-semibold">控制面</h3>
          {control.isLoading && <EmptyPanel text="加载中..." />}
          {!control.isLoading && rows.length === 0 && <EmptyPanel text="暂无 AxonHub 绑定渠道" />}
          {rows.length > 0 && <DataTable minWidthClass="min-w-[1380px]" head={<tr><Header>渠道</Header><Header>远端状态</Header><Header>当前 / 目标标签</Header><Header>成本</Header><Header>当前 / 目标权重</Header><Header>所有者</Header><Header>余额</Header><Header>漂移与操作</Header><Header align="right">操作</Header></tr>}>
            {rows.map((row) => <tr key={row.channel_id} className="border-t border-border align-top"><Cell><div className="font-medium">{row.channel_name || row.channel_id}</div><div className="text-xs text-muted-foreground">{row.card_name} · {row.model}</div></Cell><Cell><Badge variant={row.remote_status === 'enabled' ? 'success' : row.remote_status === 'archived' ? 'destructive' : 'secondary'}>{channelStatus(row.remote_status)}</Badge></Cell><Cell><div className="max-w-52 whitespace-normal break-words">{tagText(row.remote_tags)}<span className="text-muted-foreground"> / {tagText(row.target_tags)}</span></div></Cell><Cell>{row.cost_available ? row.cost : '-'}</Cell><Cell>{row.remote_weight} / {row.desired_weight || '-'}</Cell><Cell><Badge variant={row.external_takeover ? 'destructive' : row.owner === 'aum' ? 'outline' : 'secondary'}>{ownerText(row.owner, row.external_takeover)}</Badge></Cell><Cell>{row.balance_fresh ? row.balance_remain : '非新鲜'}</Cell><Cell><div className="max-w-72 whitespace-pre-wrap break-words text-xs text-muted-foreground">{row.last_error || row.last_reason || '-'}</div><div className="mt-1 text-xs text-muted-foreground">{fmtTime(row.updated_at)}</div></Cell><Cell><div className="flex justify-end">{row.external_takeover && <Button variant="ghost" size="icon" title="重新接管" onClick={() => adopt.mutate(row.channel_id)} disabled={adopt.isPending}><RotateCcw className="size-4" /><span className="sr-only">重新接管</span></Button>}</div></Cell></tr>)}
          </DataTable>}
        </section>
        <AxonHubLogs rows={control.data?.logs ?? []} />
      </>}
    </section>
  )
}

function AxonHubConfigDialog({ draft, onChange, onSave, saving }: { draft?: AxonHubConfig; onChange: (next: AxonHubConfig) => void; onSave: () => void; saving: boolean }) {
	return <Dialog><DialogTrigger asChild><Button variant="outline" size="sm"><SlidersHorizontal className="size-4" />配置</Button></DialogTrigger><DialogContent><DialogTitle>AxonHub 配置</DialogTitle>{!draft ? <EmptyPanel text="加载中..." /> : <div className="grid gap-4"><Field label="Base URL"><Input value={draft.base_url} onChange={(e) => onChange({ ...draft, base_url: e.target.value })} /></Field><Field label="管理员邮箱"><Input type="email" value={draft.admin_email} onChange={(e) => onChange({ ...draft, admin_email: e.target.value })} /></Field><Field label="管理员密码"><Input type="password" value={draft.admin_password || ''} placeholder={secretPlaceholder(draft.admin_password_set)} onChange={(e) => onChange({ ...draft, admin_password: e.target.value })} /></Field><Field label="控制模式"><Select value={draft.control_mode} onValueChange={(value: AxonHubConfig['control_mode']) => onChange({ ...draft, control_mode: value })}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="off">关闭</SelectItem><SelectItem value="observe">观察</SelectItem><SelectItem value="active">主动控制</SelectItem></SelectContent></Select></Field><Button onClick={onSave} disabled={saving}>{saving ? <Loader2 className="size-4 animate-spin" /> : <Database className="size-4" />}保存</Button></div>}</DialogContent></Dialog>
}

function PreflightPanel({ value }: { value: AxonHubPreflight }) {
  return <div className={cn('grid gap-2 border px-3 py-3 text-sm', value.ok ? 'border-success/40 bg-success/5' : 'border-destructive/40 bg-destructive/5')}>
    {value.checks.map((check) => <div key={check.name} className="grid gap-1 sm:grid-cols-[120px_1fr]"><span className={cn('font-medium', check.ok ? 'text-success' : 'text-destructive')}>{check.name}</span><span className="whitespace-normal break-words text-muted-foreground">{check.message}</span></div>)}
  </div>
}

function AxonHubLogs({ rows }: { rows: SchedulerLog[] }) {
  return <section className="grid min-w-0 gap-3"><h3 className="text-base font-semibold">操作历史</h3>{rows.length === 0 ? <EmptyPanel text="暂无操作记录" /> : <div className="grid gap-2">{rows.map((row) => <div key={row.id} className="grid min-w-0 gap-1 border border-border px-3 py-2 text-sm md:grid-cols-[150px_1fr_auto]"><span className="text-xs text-muted-foreground">{fmtTime(row.created_at)}</span><div className="min-w-0"><div className="font-medium">{row.channel_name || row.channel_id}</div><div className="whitespace-pre-wrap break-words text-xs text-muted-foreground">{row.message}</div></div><Badge variant={row.status === 'success' ? 'success' : row.status === 'error' ? 'destructive' : 'secondary'}>{row.status}</Badge></div>)}</div>}</section>
}

function axonHubOptions(channels: SchedulerChannel[], card: ModelCard, cards: ModelCard[]) {
  const used = new Set(cards.filter((item) => item.id !== card.id).map((item) => item.axonhub_channel_id).filter(Boolean))
  const available = channels.filter((channel) => !channel.archived && supportsCardModel(channel, card) && !used.has(channel.id))
  if (!card.axonhub_channel_id || available.some((channel) => channel.id === card.axonhub_channel_id)) return available
  return [{ id: card.axonhub_channel_id, name: card.axonhub_channel_name || card.axonhub_channel_id, status: 0, remote_status: 'archived' }, ...available]
}

function supportsCardModel(channel: SchedulerChannel, card: ModelCard) {
  const model = card.model.trim().toLowerCase()
  return model.startsWith('gpt-') && (channel.models ?? []).some((item) => item.trim().toLowerCase() === model)
}

function tagText(tags?: string[]) { return (tags ?? []).join(', ') || '-' }
function channelStatus(status?: string) { return (({ enabled: '启用', disabled: '关闭', archived: '归档' } as Record<string, string>)[status || ''] ?? status) || '-' }
function controlModeText(mode?: string) { return ({ active: '主动控制', observe: '观察', off: '关闭' } as Record<string, string>)[mode || ''] ?? '观察' }
function ownerText(owner: string, external: boolean) { return external ? '外部接管' : ({ aum: 'AUM', observed: '仅观察' } as Record<string, string>)[owner] ?? owner }
function Header({ children, align }: { children: ReactNode; align?: 'right' }) { return <th className={cn('px-3 py-2 font-medium', align === 'right' && 'text-right')}>{children}</th> }
function Cell({ children }: { children: ReactNode }) { return <td className="px-3 py-2 align-middle">{children}</td> }
