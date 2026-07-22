import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, AlertTriangle, CirclePause, CirclePlay, Loader2, Plus, Power, PowerOff, RefreshCcw, Settings2, ShieldAlert, ShieldCheck, SlidersHorizontal, Tags, Trash2 } from 'lucide-react'
import { ActionRow, EmptyPanel, Field, FeedbackBanner, FormError, Metric, SaveButton, StatusBadge } from '@/components/common'
import { Page, ShellLoading } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { fmtTime } from '@/lib/format'
import { alertError, secretPlaceholder, useFeedback } from '@/lib/feedback'
import { cn } from '@/lib/utils'
import type { AvailabilityPolicy, AvailabilityRow, ModelCard, SchedulerApplyResult, SchedulerChannel, SchedulerConfig, SchedulerGroup, SchedulerLog, SchedulerTier, TrafficStatus, TrafficWindow, UpstreamRow } from '@/types'

const none = '__none__'
const defaultTiers: SchedulerTier[] = [
  { tag: 'gpt_low', group: 'gpt_low', price_min: 0, price_max: 0.1, sale_price: 0.1 },
  { tag: 'gpt_stable', group: 'gpt_stable', price_min: 0, price_max: 0.25, sale_price: 0.25 },
]

export function SchedulerPage() {
  const qc = useQueryClient()
  const [cfgDraft, setCfgDraft] = useState<SchedulerConfig | null>(null)
  const fb = useFeedback()
  const cfg = useQuery({ queryKey: ['scheduler', 'config'], queryFn: () => api<SchedulerConfig>('/api/scheduler/config') })
  const cards = useQuery({ queryKey: ['cards'], queryFn: () => api<ModelCard[]>('/api/cards') })
  const logs = useQuery({ queryKey: ['scheduler', 'logs'], queryFn: () => api<SchedulerLog[]>('/api/scheduler/logs?limit=50') })
  const configured = Boolean(cfg.data?.scheduler_base_url && cfg.data.scheduler_user_id && cfg.data.scheduler_access_token_set)
  const groups = useQuery({
    queryKey: ['scheduler', 'groups'],
    queryFn: () => api<SchedulerGroup[]>('/api/scheduler/groups'),
    enabled: configured,
  })
  const channels = useQuery({
    queryKey: ['scheduler', 'channels'],
    queryFn: () => api<SchedulerChannel[]>('/api/scheduler/channels'),
    enabled: configured,
  })
  const form = cfgDraft ?? cfg.data
  const saveConfig = useMutation({
    mutationFn: () => api<SchedulerConfig>('/api/scheduler/config', { method: 'PATCH', body: JSON.stringify(form) }),
    onMutate: () => fb.pending(),
    onSuccess: async (data) => {
      fb.success()
      setCfgDraft(null)
      await qc.setQueryData(['scheduler', 'config'], data)
      void groups.refetch()
      void channels.refetch()
    },
    onError: fb.fail,
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
    onError: alertError,
  })
  const setStatus = useMutation({
    mutationFn: ({ card, status }: { card: ModelCard; status: number }) =>
      api(`/api/cards/${card.id}/scheduler/status`, { method: 'POST', body: JSON.stringify({ status }) }),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ['cards'] }),
        qc.invalidateQueries({ queryKey: ['status'] }),
        qc.invalidateQueries({ queryKey: ['scheduler', 'logs'] }),
      ])
      void channels.refetch()
    },
    onError: alertError,
  })
  const applyGroups = useMutation({
    mutationFn: async () => {
      if (!form) throw new Error('scheduler config missing')
      const saved = await api<SchedulerConfig>('/api/scheduler/config', { method: 'PATCH', body: JSON.stringify({ ...form, scheduler_tiers: schedulerTiers(form) }) })
      await qc.setQueryData(['scheduler', 'config'], saved)
      return api<SchedulerApplyResult>('/api/scheduler/groups/apply', { method: 'POST', body: JSON.stringify({}) })
    },
    onSuccess: (data) => {
      setCfgDraft(null)
      fb.setMessage(`更新 ${data.updated} 个，保持 ${data.unchanged} 个，跳过 ${data.skipped} 个`)
      void groups.refetch()
      void channels.refetch()
      void logs.refetch()
    },
    onError: fb.fail,
  })
  if (!form) return <ShellLoading />
  const rows = cards.data ?? []
  const poolRows = rows.filter((card) => card.pool_enabled ?? true)
  const list = channels.data ?? []
  const tiers = schedulerTiers(form)
  const groupRows = schedulerTierRows(tiers, groups.data ?? [], list)
  const groupOptions = schedulerGroupOptions(groups.data ?? [], list, tiers)
  const updateTier = (index: number, patch: Partial<SchedulerTier>) =>
    setCfgDraft({ ...form, scheduler_tiers: tiers.map((tier, i) => i === index ? { ...tier, ...patch } : tier) })
  const addTier = () => {
    const used = new Set(tiers.map((tier) => tier.group).filter(Boolean))
    const group = groupOptions.find((item) => !used.has(item)) ?? ''
    setCfgDraft({ ...form, scheduler_tiers: [...tiers, { tag: nextTierName(tiers, group), group, price_min: 0, price_max: 0, sale_price: schedulerSalePrice({ tag: group, group, price_min: 0, price_max: 0, sale_price: 0 }) }] })
  }
  const deleteTier = (index: number) => setCfgDraft({ ...form, scheduler_tiers: tiers.filter((_, i) => i !== index) })
  const refreshing = cfg.isFetching || cards.isFetching || channels.isFetching || groups.isFetching || logs.isFetching
  const refreshAll = () => {
    void cfg.refetch()
    void cards.refetch()
    void logs.refetch()
    if (configured) {
      void groups.refetch()
      void channels.refetch()
    }
  }
  return (
    <Page
      title="渠道管理"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {refreshing && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <Button variant="outline" size="sm" onClick={refreshAll} disabled={refreshing}>
            <RefreshCcw className={cn('size-4', refreshing && 'animate-spin')} />
            刷新
          </Button>
          <SchedulerConfigDialog
            form={form}
            groupOptions={groupOptions}
            message={fb.message}
            saveError={saveConfig.isError}
            saving={saveConfig.isPending}
            onChange={(patch) => {
              fb.clear()
              setCfgDraft({ ...form, ...patch })
            }}
            onSave={() => saveConfig.mutate()}
          />
          <SchedulerGroupDialog
            tiers={tiers}
            groupOptions={groupOptions}
            message={fb.message}
            saveError={saveConfig.isError || applyGroups.isError}
            saving={saveConfig.isPending}
            applying={applyGroups.isPending}
            onUpdateTier={updateTier}
            onAddTier={addTier}
            onDeleteTier={deleteTier}
            onApply={() => applyGroups.mutate()}
            onRefresh={() => {
              void groups.refetch()
              void channels.refetch()
            }}
          />
        </div>
      }
    >
      {configured && !(form.scheduler_unassigned_group ?? '').trim() && (
        <FeedbackBanner
          message="请先在「连接配置」中设置未分配分组，否则成本调度不会写入调度器（new-api 无法用空分组移出渠道）。"
          error
        />
      )}
      <AvailabilityControl />
      <TrafficControl />
      <Section title="调度器分组">
        <FormError error={groups.error || channels.error} />
        {groups.isLoading && <EmptyPanel text="加载中..." />}
        {!groups.isLoading && !groups.error && groupRows.length === 0 && <EmptyPanel text="暂无调度器分组" />}
        {!groups.isLoading && !groups.error && groupRows.length > 0 && (
          <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
            {groupRows.map((group) => (
              <Card key={`${group.name}-${group.title || group.name}`} className="bg-card">
                <CardHeader className="gap-2">
                  <div className="flex min-w-0 items-start justify-between gap-3">
                    <div className="min-w-0">
                      <CardTitle className="truncate">{group.title || group.name}</CardTitle>
                      <CardDescription className="truncate">调度器分组 {group.name} · {group.channels.length} 个渠道</CardDescription>
                    </div>
                    {group.ratio && <Badge variant="outline">{group.ratio}x</Badge>}
                  </div>
                </CardHeader>
                <CardContent className="grid gap-1.5">
                  {group.channels.length === 0 && <div className="text-sm text-muted-foreground">暂无渠道</div>}
                  {group.channels.length > 0 && (
                    <div className="grid max-h-72 gap-1.5 overflow-y-auto pr-1">
                      {group.channels.map((channel) => (
                        <div key={channel.id} className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 text-sm">
                          <span className="min-w-0 truncate">{channel.name || channel.id}</span>
                          <div className="flex shrink-0 items-center gap-2 text-xs">
                            <span className="text-muted-foreground">优先 {channel.priority ?? '-'} · 权重 {channel.weight ?? '-'}</span>
                            <span className={cn(channel.status === 1 ? 'text-success' : channel.status === 2 ? 'text-destructive' : 'text-muted-foreground')}>
                              {schedulerStatus(channel.status)}
                            </span>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </Section>

      <Section title="卡片绑定">
        {cards.isLoading && <EmptyPanel text="加载中..." />}
        {!cards.isLoading && poolRows.length === 0 && <EmptyPanel text="暂无号池卡片" />}
        {!cards.isLoading && poolRows.length > 0 && (
          <div className="grid min-w-0 gap-5">
            {groupCards(poolRows).map((group) => (
              <section key={group.name} className="grid min-w-0 gap-2">
                <div className="text-sm font-medium text-foreground">展示分组 · {group.name}</div>
                <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
                  {group.cards.map((card) => {
                    const options = channelsForCard(list, card, poolRows)
                    const channel = channelForCard(list, card)
                    const canToggle = channel?.status === 1 || channel?.status === 2
                    const nextStatus = channel?.status === 2 ? 1 : 2
                    const badge = schedulerChannelBadge(card, channel)
                    return (
                      <Card key={card.id} className="grid h-full min-w-0 grid-rows-[auto_1fr] bg-card">
                        <CardHeader className="gap-2">
                          <div className="flex min-w-0 items-start justify-between gap-3">
                            <div className="min-w-0">
                              <CardTitle className="truncate">{card.name}</CardTitle>
                            </div>
                            <StatusBadge ok={badge.ok} okText={badge.text} failText={badge.text} />
                          </div>
                        </CardHeader>
                        <CardContent className="grid h-full grid-rows-[auto_auto_1fr] gap-3">
                          <div className="grid grid-cols-2 gap-2">
                            <InfoCell label="上游 Key 原始分组" value={originalKeyGroup(card)} />
                            <InfoCell label="上游成本" value={upstreamCost(card)} />
                            <InfoCell label="自动命中分组" value={matchedTierLabel(card, tiers)} />
                            <InfoCell label="渠道优先级" value={channel?.priority?.toString() ?? '-'} />
                            <InfoCell label="渠道权重" value={channel?.weight?.toString() ?? '-'} />
                            <InfoCell label="自动恢复" value={schedulerRestoreState(card, channel)} />
                          </div>
                          <Field label="绑定渠道">
                            <Select
                              value={card.scheduler_channel_id || none}
                              onValueChange={(value) => bind.mutate({ card, channel: value === none ? undefined : options.find((item) => item.id === value) })}
                            >
                              <SelectTrigger>
                                <SelectValue placeholder="选择渠道" />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value={none}>不绑定</SelectItem>
                                {options.map((channel) => (
                                  <SelectItem key={channel.id} value={channel.id}>
                                    {channel.name || channel.id}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </Field>
                          <Button
                            variant={!canToggle ? 'outline' : nextStatus === 1 ? 'default' : 'danger'}
                            size="sm"
                            className={cn('self-end', canToggle && nextStatus === 1 && 'bg-success text-primary-foreground hover:bg-success/90 focus-visible:ring-success/20')}
                            disabled={!card.scheduler_channel_id || !canToggle || setStatus.isPending}
                            onClick={() => setStatus.mutate({ card, status: nextStatus })}
                          >
                            {setStatus.isPending ? <Loader2 className="size-4 animate-spin" /> : nextStatus === 1 ? <Power className="size-4" /> : <PowerOff className="size-4" />}
                            {!canToggle ? '刷新渠道' : nextStatus === 1 ? '启用渠道' : '关闭渠道'}
                          </Button>
                        </CardContent>
                      </Card>
                    )
                  })}
                </div>
              </section>
            ))}
          </div>
        )}
      </Section>

      <Section title="调度日志">
        <Card className="min-w-0 bg-card">
          <CardHeader>
            <div className="flex items-center justify-between gap-3">
              <div>
                <CardTitle>调度日志</CardTitle>
                <CardDescription>成本调度、自动关闭和恢复渠道的执行记录</CardDescription>
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
                  <div className="truncate font-medium">{schedulerLogTitle(log)}</div>
                  <div className="truncate text-xs text-muted-foreground">{log.message}</div>
                </div>
                <span className={cn('text-xs font-medium', log.status === 'success' ? 'text-emerald-600' : log.status === 'error' ? 'text-destructive' : 'text-muted-foreground')}>
                  {logAction(log.action)} · {logStatus(log.status)}
                </span>
              </div>
            ))}
          </CardContent>
        </Card>
      </Section>
    </Page>
  )
}

function TrafficControl() {
  const qc = useQueryClient()
  const status = useQuery({
    queryKey: ['scheduler', 'traffic', 'status'],
    queryFn: () => api<TrafficStatus>('/api/scheduler/traffic/status'),
    refetchInterval: 5000,
  })
  const reconcile = useMutation({
    mutationFn: () => api('/api/scheduler/traffic/reconcile', { method: 'POST' }),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['scheduler', 'traffic'] }) },
    onError: alertError,
  })
  const baseline = useMutation({
    mutationFn: (channelID: string) => api(`/api/scheduler/traffic/${channelID}/baseline`, { method: 'POST' }),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['scheduler', 'traffic'] }) },
    onError: alertError,
  })
  const data = status.data
  const rows = data?.channels ?? []
  const healthy = rows.filter((row) => row.state === 'healthy').length
  const degraded = rows.filter((row) => ['warning', 'probe_required', 'degraded'].includes(row.state)).length
  const blocked = rows.filter((row) => ['soft_blocked', 'hard_blocked', 'recovering', 'hard_recovering', 'external_disabled'].includes(row.state)).length
  return (
    <Section title="真实流量调度">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="遥测" value={data?.connected ? '已连接' : data?.mode === 'off' ? '已关闭' : '异常'} accent={data?.connected ? 'success' : data?.mode === 'off' ? undefined : 'danger'} />
        <Metric label="延迟" value={data ? `${data.lag_seconds}s` : '-'} accent={data && data.lag_seconds > 15 ? 'danger' : undefined} />
        <Metric label="健康" value={healthy} accent="success" />
        <Metric label="退化" value={degraded} accent={degraded > 0 ? 'danger' : undefined} />
        <Metric label="熔断" value={blocked} accent={blocked > 0 ? 'danger' : undefined} />
      </div>
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Activity className="size-4" />
          <span>{trafficModeLabel(data?.mode)}</span>
          {data?.frozen && <Badge variant="destructive">冻结 · {data.freeze_reason || '遥测过期'}</Badge>}
          {!data?.frozen && data?.backlog_pages ? <Badge variant="outline">积压 {data.backlog_pages}</Badge> : null}
        </div>
        <Button variant="outline" size="icon" title="立即拉取并协调" onClick={() => reconcile.mutate()} disabled={reconcile.isPending || data?.mode === 'off'}>
          <RefreshCcw className={cn('size-4', reconcile.isPending && 'animate-spin')} />
          <span className="sr-only">立即拉取并协调</span>
        </Button>
      </div>
      <FormError error={status.error} />
      {status.isLoading && <EmptyPanel text="加载中..." />}
      {!status.isLoading && rows.length === 0 && <EmptyPanel text="暂无真实流量遥测" />}
      {!status.isLoading && rows.length > 0 && (
        <div className="overflow-x-auto border border-border">
          <table className="w-full min-w-[1180px] text-left text-sm">
            <thead className="border-b border-border bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">渠道</th>
                <th className="px-3 py-2 font-medium">状态</th>
                <th className="px-3 py-2 font-medium">模型</th>
                <th className="px-3 py-2 font-medium">15 秒</th>
                <th className="px-3 py-2 font-medium">1 分钟</th>
                <th className="px-3 py-2 font-medium">5 分钟</th>
                <th className="px-3 py-2 font-medium">优先级</th>
                <th className="px-3 py-2 font-medium">权重</th>
                <th className="px-3 py-2 font-medium">原因</th>
                <th className="px-3 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {rows.map((row) => (
                <tr key={row.channel_id}>
                  <td className="px-3 py-2"><div className="font-medium">{row.channel_name || row.channel_id}</div><div className="text-xs text-muted-foreground">健康 {Math.round(row.health_score)} · 基准 {row.healthy_baseline_ttft_ms || '-'}ms</div></td>
                  <td className="px-3 py-2"><Badge variant={trafficStateVariant(row.state)}>{trafficStateLabel(row.state)}</Badge></td>
                  <td className="px-3 py-2">{row.model || '-'}</td>
                  <td className="px-3 py-2">{trafficWindowLabel(row.window_15s)}</td>
                  <td className="px-3 py-2">{trafficWindowLabel(row.window_1m)}</td>
                  <td className="px-3 py-2">{trafficWindowLabel(row.window_5m)}</td>
                  <td className="px-3 py-2">{row.base_priority} → {row.actual_priority}</td>
                  <td className="px-3 py-2">{row.base_weight} → {row.actual_weight}</td>
                  <td className="max-w-64 px-3 py-2 text-xs text-muted-foreground"><span className="block truncate" title={row.reason}>{row.reason || '-'}</span></td>
                  <td className="px-3 py-2 text-right">
                    {row.managed ? (
                      <Button variant="ghost" size="sm" onClick={() => baseline.mutate(row.channel_id)} disabled={baseline.isPending}>采纳基准</Button>
                    ) : <span className="text-xs text-muted-foreground">仅展示</span>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  )
}

function trafficModeLabel(mode?: TrafficStatus['mode']) {
  if (mode === 'active') return '主动控制'
  if (mode === 'observe') return '仅观察'
  return '已关闭'
}

function trafficStateLabel(state: string) {
  return ({ healthy: '健康', warning: '降权', probe_required: '待探测', degraded: '严重降权', soft_blocked: '软熔断', hard_blocked: '硬关闭', recovering: '阶梯恢复', hard_recovering: '恢复确认', external_disabled: '调度器自动关闭', unmanaged: '未绑定' } as Record<string, string>)[state] ?? state
}

function trafficStateVariant(state: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (['soft_blocked', 'hard_blocked'].includes(state)) return 'destructive'
  if (['warning', 'degraded', 'probe_required', 'recovering', 'hard_recovering', 'external_disabled'].includes(state)) return 'secondary'
  if (state === 'unmanaged') return 'outline'
  return 'default'
}

function trafficWindowLabel(window?: TrafficWindow) {
  if (!window || window.requests === 0) return '-'
  const successRate = Math.round((1 - window.failure_rate) * 100)
  const p95 = window.p95_ttft_ms > 0 ? `${window.p95_ttft_ms}ms` : '-'
  return `${window.requests} 次 · 成功 ${successRate}% · P95 ${p95}`
}

function AvailabilityControl() {
  const qc = useQueryClient()
  const [upstreamID, setUpstreamID] = useState(() => new URLSearchParams(location.search).get('upstream_id') || 'all')
  const [state, setState] = useState('all')
  const [displayGroup, setDisplayGroup] = useState('all')
  const [policyUpstreamID, setPolicyUpstreamID] = useState('')
  const [policyDraft, setPolicyDraft] = useState<AvailabilityPolicy | null>(null)
  const availability = useQuery({ queryKey: ['scheduler', 'availability'], queryFn: () => api<AvailabilityRow[]>('/api/scheduler/availability'), refetchInterval: 60000 })
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamRow[]>('/api/upstreams') })
  const cards = useQuery({ queryKey: ['cards'], queryFn: () => api<ModelCard[]>('/api/cards') })
  const policy = useQuery({
    queryKey: ['availability-policy', policyUpstreamID],
    queryFn: () => api<AvailabilityPolicy>(`/api/upstreams/${policyUpstreamID}/availability-policy`),
    enabled: Boolean(policyUpstreamID),
  })
  const refresh = useMutation({
    mutationFn: () => api('/api/scheduler/availability/reconcile', { method: 'POST' }),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['scheduler', 'availability'] }) },
    onError: alertError,
  })
  const action = useMutation({
    mutationFn: ({ cardID, action }: { cardID: string; action: 'force_enable' | 'hold_off' | 'release_hold' | 'check_now' }) =>
      api(`/api/scheduler/availability/${cardID}/action`, { method: 'POST', body: JSON.stringify({ action, minutes: 30 }) }),
    onSuccess: async () => {
      await Promise.all([qc.invalidateQueries({ queryKey: ['scheduler', 'availability'] }), qc.invalidateQueries({ queryKey: ['cards'] })])
    },
    onError: alertError,
  })
  const savePolicy = useMutation({
    mutationFn: () => api<AvailabilityPolicy>(`/api/upstreams/${policyUpstreamID}/availability-policy`, { method: 'PATCH', body: JSON.stringify(policyDraft ?? policy.data) }),
    onSuccess: async (next) => {
      setPolicyDraft(next)
      await Promise.all([
        qc.invalidateQueries({ queryKey: ['availability-policy', policyUpstreamID] }),
        qc.invalidateQueries({ queryKey: ['scheduler', 'availability'] }),
        qc.invalidateQueries({ queryKey: ['balances'] }),
      ])
    },
    onError: alertError,
  })
  const upstreamRows = upstreams.data ?? []
  const cardsByID = new Map((cards.data ?? []).map((card) => [card.id, card]))
  const groups = Array.from(new Set(Array.from(cardsByID.values()).map((card) => card.display_group?.trim() || '其他'))).sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
  const rows = (availability.data ?? []).filter((row) => {
    const card = cardsByID.get(row.card_id)
    const group = card?.display_group?.trim() || '其他'
    return (upstreamID === 'all' || row.upstream_id === upstreamID) && (state === 'all' || row.state === state) && (displayGroup === 'all' || group === displayGroup)
  })
  const healthy = rows.filter((row) => row.state === 'healthy').length
  const risk = rows.filter((row) => ['warning', 'blocked', 'action_failed', 'recovering'].includes(row.state)).length
  const autoClosed = rows.filter((row) => ['blocked', 'recovering', 'external_disabled'].includes(row.state) && (row.actual_status === 2 || row.actual_status === 3)).length
  const manualClosed = rows.filter((row) => row.state === 'manual_off').length
  const unmanaged = rows.filter((row) => row.state === 'unmanaged').length
  const draft = policyDraft ?? policy.data

  return (
    <Section title="渠道可用性">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="可用" value={healthy} accent="success" />
        <Metric label="风险" value={risk} accent={risk > 0 ? 'danger' : undefined} />
        <Metric label="自动关闭" value={autoClosed} accent={autoClosed > 0 ? 'danger' : undefined} />
        <Metric label="手动关闭" value={manualClosed} />
        <Metric label="未绑定" value={unmanaged} />
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <Select value={upstreamID} onValueChange={setUpstreamID}>
          <SelectTrigger className="w-44"><SelectValue placeholder="上游" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部上游</SelectItem>
            {upstreamRows.map(({ upstream }) => <SelectItem key={upstream.id} value={upstream.id}>{upstream.name}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={state} onValueChange={setState}>
          <SelectTrigger className="w-36"><SelectValue placeholder="状态" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {availabilityStates.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={displayGroup} onValueChange={setDisplayGroup}>
          <SelectTrigger className="w-36"><SelectValue placeholder="展示分组" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部展示分组</SelectItem>
            {groups.map((group) => <SelectItem key={group} value={group}>{group}</SelectItem>)}
          </SelectContent>
        </Select>
        <Button variant="outline" size="icon" title="重新协调渠道" onClick={() => refresh.mutate()} disabled={refresh.isPending}>
          <RefreshCcw className={cn('size-4', refresh.isPending && 'animate-spin')} />
          <span className="sr-only">重新协调渠道</span>
        </Button>
      </div>
      <FormError error={availability.error || upstreams.error || cards.error} />
      {availability.isLoading && <EmptyPanel text="加载中..." />}
      {!availability.isLoading && rows.length === 0 && <EmptyPanel text="暂无受管理渠道" />}
      {!availability.isLoading && rows.length > 0 && (
        <div className="overflow-x-auto border border-border">
          <table className="w-full min-w-[1060px] text-left text-sm">
            <thead className="border-b border-border bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">渠道</th>
                <th className="px-3 py-2 font-medium">上游</th>
                <th className="px-3 py-2 font-medium">展示分组</th>
                <th className="px-3 py-2 font-medium">状态</th>
                <th className="px-3 py-2 font-medium">原因</th>
                <th className="px-3 py-2 font-medium">余额</th>
                <th className="px-3 py-2 font-medium">恢复</th>
                <th className="px-3 py-2 font-medium">实际</th>
                <th className="px-3 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const card = cardsByID.get(row.card_id)
                const forceActive = row.override === 'force_enable' || row.state === 'forced_on'
                const canForceEnable = row.managed && !action.isPending && !forceActive && (row.actual_status !== 1 || row.state === 'blocked' || row.state === 'action_failed')
                return (
                  <tr key={row.channel_id} className="border-b border-border last:border-0">
                    <td className="max-w-52 px-3 py-2"><div className="truncate font-medium" title={row.channel_name || row.channel_id}>{row.channel_name || row.channel_id}</div><div className="truncate text-xs text-muted-foreground">{row.card_name}</div></td>
                    <td className="max-w-40 px-3 py-2"><div className="truncate">{row.upstream_name || '-'}</div><button className="text-xs text-primary hover:underline" onClick={() => { setPolicyUpstreamID(row.upstream_id); setPolicyDraft(null) }}>策略</button></td>
                    <td className="px-3 py-2 whitespace-nowrap">{card?.display_group?.trim() || '其他'}</td>
                    <td className="px-3 py-2"><AvailabilityStateBadge state={row.state} /></td>
                    <td className="max-w-52 px-3 py-2"><div className="truncate" title={availabilityReason(row)}>{availabilityReason(row)}</div>{row.last_error && <div className="truncate text-xs text-destructive" title={row.last_error}>{row.last_error}</div>}</td>
                    <td className="px-3 py-2 whitespace-nowrap"><div>{row.balance_fresh ? `${row.balance_remain?.toFixed(2) ?? '-'} 元` : '数据陈旧'}</div>{row.runway.warning && <div className="text-xs text-warning">约 {row.runway.hours_remaining?.toFixed(1)} 小时</div>}</td>
                    <td className="px-3 py-2 whitespace-nowrap">{row.disabled_at ? `${row.recovery_success_count}/3` : '-'}</td>
                    <td className="px-3 py-2 whitespace-nowrap">{channelActualState(row.actual_status)}</td>
                    <td className="px-3 py-2"><div className="flex justify-end gap-1">
                      {row.state === 'manual_off' ? (
                        <Button variant="outline" size="icon" title="解除手动关闭" disabled={action.isPending} onClick={() => action.mutate({ cardID: row.card_id, action: 'release_hold' })}><CirclePlay className="size-4" /><span className="sr-only">解除手动关闭</span></Button>
                      ) : (
                        <Button variant="outline" size="icon" title="手动关闭渠道" disabled={action.isPending || !row.managed} onClick={() => action.mutate({ cardID: row.card_id, action: 'hold_off' })}><CirclePause className="size-4" /><span className="sr-only">手动关闭渠道</span></Button>
                      )}
                      {forceActive ? (
                        <Button variant="outline" size="icon" title="结束限时接管" disabled={action.isPending || !row.managed} onClick={() => action.mutate({ cardID: row.card_id, action: 'release_hold' })}><ShieldAlert className="size-4" /><span className="sr-only">结束限时接管</span></Button>
                      ) : canForceEnable ? (
                        <Button variant="outline" size="icon" title="限时启用 30 分钟" onClick={() => action.mutate({ cardID: row.card_id, action: 'force_enable' })}><ShieldCheck className="size-4" /><span className="sr-only">限时启用 30 分钟</span></Button>
                      ) : null}
                      <Button variant="outline" size="icon" title="立即检查" disabled={action.isPending || !row.managed} onClick={() => action.mutate({ cardID: row.card_id, action: 'check_now' })}><RefreshCcw className="size-4" /><span className="sr-only">立即检查</span></Button>
                    </div></td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      <Dialog open={Boolean(policyUpstreamID)} onOpenChange={(open) => { if (!open) { setPolicyUpstreamID(''); setPolicyDraft(null) } }}>
        <DialogContent>
          <DialogTitle>余额保护策略</DialogTitle>
          {!draft && <EmptyPanel text="加载中..." />}
          {draft && <div className="grid gap-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="余额保护">
                <Select value={draft.balance_guard_mode} onValueChange={(value: 'observe' | 'active') => setPolicyDraft({ ...draft, balance_guard_mode: value })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent><SelectItem value="observe">观察模式</SelectItem><SelectItem value="active">自动摘流</SelectItem></SelectContent>
                </Select>
              </Field>
              <Field label="耗尽预警（小时）"><Input type="number" min="1" value={draft.runway_warning_hours} onChange={(e) => setPolicyDraft({ ...draft, runway_warning_hours: Number(e.target.value || 0) })} /></Field>
              <Field label="预警线（元）"><Input type="number" min="0" value={draft.low_balance_threshold} onChange={(e) => setPolicyDraft({ ...draft, low_balance_threshold: Number(e.target.value || 0) })} /></Field>
              <Field label="关闭线（元）"><Input type="number" min="0" value={draft.balance_close_threshold} onChange={(e) => setPolicyDraft({ ...draft, balance_close_threshold: Number(e.target.value || 0) })} /></Field>
              <Field label="恢复线（元）"><Input type="number" min="0" value={draft.balance_recover_threshold} onChange={(e) => setPolicyDraft({ ...draft, balance_recover_threshold: Number(e.target.value || 0) })} /></Field>
            </div>
            <ActionRow><Button onClick={() => savePolicy.mutate()} disabled={savePolicy.isPending}>{savePolicy.isPending ? <Loader2 className="size-4 animate-spin" /> : <ShieldAlert className="size-4" />}保存策略</Button></ActionRow>
          </div>}
        </DialogContent>
      </Dialog>
    </Section>
  )
}

const availabilityStates = [
  { value: 'healthy', label: '可用' }, { value: 'warning', label: '风险' }, { value: 'blocked', label: '自动关闭' }, { value: 'recovering', label: '恢复中' }, { value: 'external_disabled', label: '调度器自动关闭' }, { value: 'manual_off', label: '手动关闭' }, { value: 'forced_on', label: '限时启用' }, { value: 'action_failed', label: '动作失败' }, { value: 'unmanaged', label: '未绑定' },
]

function AvailabilityStateBadge({ state }: { state: string }) {
  const item = availabilityStates.find((candidate) => candidate.value === state)
  const tone = state === 'healthy' ? 'text-success' : state === 'action_failed' || state === 'blocked' ? 'text-destructive' : state === 'warning' || state === 'recovering' || state === 'external_disabled' ? 'text-warning' : 'text-muted-foreground'
  return <span className={cn('inline-flex items-center gap-1 whitespace-nowrap text-xs font-medium', tone)}>{state === 'healthy' ? <ShieldCheck className="size-3.5" /> : <AlertTriangle className="size-3.5" />}{item?.label ?? state}</span>
}

function availabilityReason(row: AvailabilityRow) {
  if (row.blockers.length > 0) return row.blockers.map((blocker) => blocker.message || availabilityBlockerText(blocker.kind)).join('；')
  if (row.override === 'manual_hold') return '人工保持关闭'
  if (row.override === 'force_enable') return `人工接管至 ${fmtTime(row.override_until)}`
  if (row.state === 'external_disabled') return '由调度器自动禁用，等待调度器自身恢复'
  return row.managed ? '-' : '未受 AUM 管理'
}

function availabilityBlockerText(kind: string) {
  if (kind === 'balance_low') return '余额达到关闭线'
  if (kind === 'quota_exhausted') return '额度耗尽'
  if (kind === 'probe_failed') return '探测确认失败'
  return kind
}

function channelActualState(status: number) {
  if (status === 1) return '启用'
  if (status === 2) return '关闭'
  if (status === 3) return '自动关闭'
  return '未知'
}

function SchedulerConfigDialog({
  form,
  groupOptions,
  message,
  saveError,
  saving,
  onChange,
  onSave,
}: {
  form: SchedulerConfig
  groupOptions: string[]
  message: string
  saveError: boolean
  saving: boolean
  onChange: (patch: Partial<SchedulerConfig>) => void
  onSave: () => void
}) {
  const unassigned = form.scheduler_unassigned_group ?? ''
  const tierGroups = new Set((form.scheduler_tiers ?? []).map((t) => t.group.trim()).filter(Boolean))
  const unassignedOptions = groupOptions.filter((g) => !tierGroups.has(g))
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
            <Input
              type="password"
              value={form.scheduler_access_token}
              placeholder={secretPlaceholder(form.scheduler_access_token_set)}
              onChange={(e) => onChange({ scheduler_access_token: e.target.value })}
            />
          </Field>
          <Field label="未分配分组">
            <Select
              value={unassigned || none}
              onValueChange={(value) => onChange({ scheduler_unassigned_group: value === none ? '' : value })}
            >
              <SelectTrigger>
                <SelectValue placeholder="选择调度器中的分组" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={none}>未设置</SelectItem>
                {unassigned && !unassignedOptions.includes(unassigned) && (
                  <SelectItem value={unassigned}>{unassigned}</SelectItem>
                )}
                {unassignedOptions.map((group) => (
                  <SelectItem key={group} value={group}>
                    {group}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="真实流量模式">
            <Select value={form.scheduler_traffic_mode || 'off'} onValueChange={(value) => onChange({ scheduler_traffic_mode: value as SchedulerConfig['scheduler_traffic_mode'] })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="off">关闭</SelectItem>
                <SelectItem value="observe">仅观察</SelectItem>
                <SelectItem value="active">主动控制</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="日志轮询（秒）">
            <Input type="number" min="1" max="60" value={form.scheduler_log_poll_seconds || 5} onChange={(e) => onChange({ scheduler_log_poll_seconds: Number(e.target.value || 5), scheduler_traffic_profile: 'balanced' })} />
          </Field>
        </div>
        <p className="text-xs text-muted-foreground">
          价格未命中任何托管档位时，渠道会写入此分组（new-api 不支持空分组）。选项来自调度器已有分组，并排除价格档位已占用的组；保存与成本调度前必填。
        </p>
        <FeedbackBanner message={message} error={saveError} />
        <ActionRow>
          <SaveButton onClick={onSave} pending={saving} message={message} label="保存配置" />
        </ActionRow>
      </DialogContent>
    </Dialog>
  )
}

function SchedulerGroupDialog({
  tiers,
  groupOptions,
  message,
  saveError,
  saving,
  applying,
  onUpdateTier,
  onAddTier,
  onDeleteTier,
  onApply,
  onRefresh,
}: {
  tiers: SchedulerTier[]
  groupOptions: string[]
  message: string
  saveError: boolean
  saving: boolean
  applying: boolean
  onUpdateTier: (index: number, patch: Partial<SchedulerTier>) => void
  onAddTier: () => void
  onDeleteTier: (index: number) => void
  onApply: () => void
  onRefresh: () => void
}) {
  const usedGroups = new Set(tiers.map((tier) => tier.group).filter(Boolean))
  const canAdd = groupOptions.some((group) => !usedGroups.has(group))
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <SlidersHorizontal className="size-4" />
          分组配置
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>分组配置</DialogTitle>
        <div className="grid gap-4">
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="outline" size="sm" onClick={onRefresh}>
              <RefreshCcw className="size-4" />
              刷新分组
            </Button>
            <Button variant="outline" size="sm" onClick={onAddTier} disabled={!canAdd}>
              <Plus className="size-4" />
              新增
            </Button>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            {tiers.map((tier, index) => (
              <div key={index} className="grid min-w-0 gap-3 rounded-md border border-border bg-background p-3">
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <Field label="名称">
                    <Input value={tier.tag} onChange={(e) => onUpdateTier(index, { tag: e.target.value })} />
                  </Field>
                  <Button variant="ghost" size="icon" title="删除" onClick={() => onDeleteTier(index)}>
                    <Trash2 className="size-4" />
                    <span className="sr-only">删除</span>
                  </Button>
                </div>
                <Field label="调度器分组">
                  <Select value={tier.group || none} onValueChange={(value) => onUpdateTier(index, { group: value === none ? '' : value })}>
                    <SelectTrigger>
                      <SelectValue placeholder="选择调度器分组" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={none}>未选择</SelectItem>
                      {groupOptions.map((group) => (
                        <SelectItem key={group} value={group} disabled={tier.group !== group && usedGroups.has(group)}>
                          {group}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                <div className="grid grid-cols-2 gap-2">
                  <Field label="上游成本下限">
                    <Input type="number" step="0.01" min="0" value={tier.price_min} onChange={(e) => onUpdateTier(index, { price_min: Number(e.target.value || 0) })} />
                  </Field>
                  <Field label="上游成本上限">
                    <Input type="number" step="0.01" min="0" value={tier.price_max} onChange={(e) => onUpdateTier(index, { price_max: Number(e.target.value || 0) })} />
                  </Field>
                </div>
                <Field label="对外售价（元/刀）">
                  <Input type="number" step="0.01" min="0" value={tier.sale_price} onChange={(e) => onUpdateTier(index, { sale_price: Number(e.target.value || 0) })} />
                </Field>
              </div>
            ))}
          </div>
          <FeedbackBanner message={message} error={saveError} />
          <ActionRow>
            <Button onClick={onApply} disabled={applying || saving}>
              {applying ? <Loader2 className="size-4 animate-spin" /> : <Tags className="size-4" />}
              {applying ? '应用中' : '应用成本调度'}
            </Button>
          </ActionRow>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid min-w-0 gap-3">
      <h2 className="font-display text-2xl font-normal leading-tight tracking-tight">{title}</h2>
      {children}
    </section>
  )
}

function InfoCell({ label, value }: { label: string; value: string }) {
  const text = value || '-'
  return (
    <div className="min-h-[76px] min-w-0 rounded-md border border-border bg-background px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 whitespace-normal break-words text-sm font-medium leading-snug" title={text}>{text}</div>
    </div>
  )
}

function channelsForCard(channels: SchedulerChannel[], card: ModelCard, cards: ModelCard[]) {
  const used = new Set(cards.filter((item) => item.id !== card.id).map((item) => item.scheduler_channel_id).filter(Boolean))
  const available = channels.filter((channel) => !used.has(channel.id))
  if (!card.scheduler_channel_id || available.some((item) => item.id === card.scheduler_channel_id)) return available
  return [{ id: card.scheduler_channel_id, name: card.scheduler_channel_name || card.scheduler_channel_id, status: -1 }, ...available]
}

function channelForCard(channels: SchedulerChannel[], card: ModelCard) {
  if (!card.scheduler_channel_id) return undefined
  return channels.find((item) => item.id === card.scheduler_channel_id) ?? { id: card.scheduler_channel_id, name: card.scheduler_channel_name || card.scheduler_channel_id, status: -1 }
}

function schedulerTiers(cfg: SchedulerConfig) {
  return Array.isArray(cfg.scheduler_tiers) ? cfg.scheduler_tiers.map((tier) => ({ ...tier, sale_price: schedulerSalePrice(tier) })) : defaultTiers
}

function schedulerSalePrice(tier: SchedulerTier) {
  if (tier.sale_price > 0) return tier.sale_price
  const keys = [tier.tag.trim(), tier.group.trim()]
  return defaultTiers.find((item) => keys.includes(item.tag) || keys.includes(item.group))?.sale_price ?? tier.price_max
}

function schedulerGroupOptions(groups: SchedulerGroup[], channels: SchedulerChannel[], tiers: SchedulerTier[] = []) {
  return Array.from(new Set([...groups.map((group) => group.name), ...channels.flatMap(channelGroups), ...tiers.map((tier) => tier.group)])).filter(Boolean).sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
}

function nextTierName(tiers: SchedulerTier[], group: string) {
  const used = new Set(tiers.map((tier) => tier.tag))
  let name = group || 'new_group'
  for (let i = 2; used.has(name); i += 1) name = `${group || 'new_group'}_${i}`
  return name
}

function groupCards(cards: ModelCard[]) {
  const groups: { name: string; cards: ModelCard[] }[] = []
  for (const card of cards) {
    const name = card.display_group?.trim() || '其他'
    let group = groups.find((item) => item.name === name)
    if (!group) {
      group = { name, cards: [] }
      groups.push(group)
    }
    group.cards.push(card)
  }
  return groups
}

type SchedulerGroupRow = SchedulerGroup & { channels: SchedulerChannel[]; title?: string }

function schedulerTierRows(tiers: SchedulerTier[], groups: SchedulerGroup[], channels: SchedulerChannel[]): SchedulerGroupRow[] {
  const byName = new Map(groups.map((group) => [group.name, group]))
  return tiers.flatMap((tier) => {
    const name = tier.group.trim()
    if (!name) return []
    const group = byName.get(name) ?? { name }
    return [{ ...group, title: tier.tag.trim() || name, channels: channels.filter((channel) => channelGroups(channel).includes(name)) }]
  })
}

function channelGroups(channel: SchedulerChannel) {
  return (channel.group ?? '').split(/[,，、;；\s]+/).map((item) => item.trim()).filter(Boolean)
}

function originalKeyGroup(card: ModelCard) {
  return card.key_group || (card.base_url ? '自定义' : '-')
}

function upstreamCost(card: ModelCard) {
  return card.effective_ratio || '-'
}

function matchedTierLabel(card: ModelCard, tiers: SchedulerTier[]) {
  const raw = card.effective_ratio?.trim()
  if (!raw) return '成本缺失'
  const price = Number(raw)
  if (!Number.isFinite(price)) return '成本缺失'
  const matched = tiers.filter((tier) => {
    const group = tier.group.trim()
    return group && price >= tier.price_min && price <= tier.price_max
  })
  if (matched.length === 0) return '未命中'
  return matched.map((tier) => {
    const tag = tier.tag.trim()
    const group = tier.group.trim()
    return tag && tag !== group ? `${tag} (${group})` : group
  }).join('，')
}

function schedulerChannelBadge(card: ModelCard, channel?: SchedulerChannel) {
  if (!card.scheduler_channel_id) return { ok: false, text: '未绑定' }
  if (channel?.status === 1) return { ok: true, text: '渠道已启用' }
  if (channel?.status === 2) return { ok: false, text: '渠道已关闭' }
  return { ok: false, text: '状态未知' }
}

function schedulerRestoreState(card: ModelCard, channel?: SchedulerChannel) {
  if (!card.scheduler_channel_id) return '-'
  if (card.scheduler_auto_disabled) return '待自动恢复'
  if (channel?.status === 2) return '外部/手动关闭'
  if (channel?.status === 1) return '未触发'
  return '状态未知'
}

function schedulerStatus(status: number) {
  if (status === 1) return '已启用'
  if (status === 2) return '已关闭'
  return '未知'
}

function logAction(action: SchedulerLog['action']) {
  if (action === 'group_sync') return '成本调度'
  return action === 'restore' ? '恢复' : '关闭'
}

function schedulerLogTitle(log: SchedulerLog) {
  if (log.action === 'group_sync') return '成本调度变更'
  const card = log.card_name || log.card_id
  const channel = log.channel_name || log.channel_id
  if (card && channel && card !== channel) {
    const normalizedCard = normalizeSchedulerName(card)
    const normalizedChannel = normalizeSchedulerName(channel)
    if (normalizedCard && normalizedChannel && (normalizedCard === normalizedChannel || normalizedCard.includes(normalizedChannel) || normalizedChannel.includes(normalizedCard))) {
      return card.length >= channel.length ? card : channel
    }
    return `${card} · ${channel}`
  }
  return card || channel || '未知渠道'
}

function normalizeSchedulerName(name: string) {
  return name.toLowerCase().replace(/[^a-z0-9\u4e00-\u9fa5]+/g, '')
}

function logStatus(status: SchedulerLog['status']) {
  if (status === 'success') return '成功'
  if (status === 'error') return '失败'
  return '跳过'
}
