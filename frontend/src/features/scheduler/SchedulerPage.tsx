import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, Power, PowerOff, RefreshCcw, Save, Settings2, SlidersHorizontal, Tags, Trash2 } from 'lucide-react'
import { EmptyPanel, Field, FormError, StatusBadge } from '@/components/common'
import { Page, ShellLoading } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage, fmtTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ModelCard, SchedulerApplyResult, SchedulerChannel, SchedulerConfig, SchedulerGroup, SchedulerLog, SchedulerTier } from '@/types'

const none = '__none__'
const defaultTiers: SchedulerTier[] = [
  { tag: 'gpt_low', group: 'gpt_low', price_min: 0, price_max: 0.1, sale_price: 0.1 },
  { tag: 'gpt_stable', group: 'gpt_stable', price_min: 0, price_max: 0.25, sale_price: 0.25 },
]

export function SchedulerPage() {
  const qc = useQueryClient()
  const [cfgDraft, setCfgDraft] = useState<SchedulerConfig | null>(null)
  const [message, setMessage] = useState('')
  const cfg = useQuery({ queryKey: ['scheduler', 'config'], queryFn: () => api<SchedulerConfig>('/api/scheduler/config') })
  const cards = useQuery({ queryKey: ['cards'], queryFn: () => api<ModelCard[]>('/api/cards') })
  const logs = useQuery({ queryKey: ['scheduler', 'logs'], queryFn: () => api<SchedulerLog[]>('/api/scheduler/logs?limit=50') })
  const configured = Boolean(cfg.data?.scheduler_base_url && cfg.data.scheduler_user_id && cfg.data.scheduler_access_token)
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
    onSuccess: async (data) => {
      setMessage('已保存')
      setCfgDraft(null)
      await qc.setQueryData(['scheduler', 'config'], data)
      void groups.refetch()
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
    onError: (error) => window.alert(errorMessage(error)),
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
      setMessage(`更新 ${data.updated} 个，保持 ${data.unchanged} 个，跳过 ${data.skipped} 个`)
      void groups.refetch()
      void channels.refetch()
      void logs.refetch()
    },
    onError: (error) => setMessage(errorMessage(error)),
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
      title="调度器"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {refreshing && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <Button variant="outline" size="sm" onClick={refreshAll} disabled={refreshing}>
            <RefreshCcw className={cn('size-4', refreshing && 'animate-spin')} />
            刷新
          </Button>
          <SchedulerConfigDialog
            form={form}
            message={message}
            saveError={saveConfig.isError}
            saving={saveConfig.isPending}
            onChange={(patch) => setCfgDraft({ ...form, ...patch })}
            onSave={() => saveConfig.mutate()}
          />
          <SchedulerGroupDialog
            tiers={tiers}
            groupOptions={groupOptions}
            message={message}
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
                        <div key={channel.id} className="flex min-w-0 items-center justify-between gap-2 rounded-sm border border-border bg-background px-2.5 py-1.5 text-sm">
                          <span className="min-w-0 truncate">{channel.name || channel.id}</span>
                          <span className={cn('shrink-0 text-xs', channel.status === 1 ? 'text-success' : channel.status === 2 ? 'text-destructive' : 'text-muted-foreground')}>
                            {schedulerStatus(channel.status)}
                          </span>
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
                <CardDescription>自动分组、自动关闭和恢复渠道的执行记录</CardDescription>
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
              <div key={index} className="grid min-w-0 gap-3 rounded-sm border border-border bg-background p-3">
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
          {message && (
            <div className={cn('rounded-sm border px-3 py-2 text-sm', saveError ? 'border-destructive/30 bg-destructive/10 text-destructive' : 'border-success/30 bg-success/10 text-success')}>
              {message}
            </div>
          )}
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button onClick={onApply} disabled={applying || saving}>
              {applying ? <Loader2 className="size-4 animate-spin" /> : <Tags className="size-4" />}
              {applying ? '应用中' : '应用分组'}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid min-w-0 gap-3">
      <h2 className="font-display text-2xl font-normal leading-tight">{title}</h2>
      {children}
    </section>
  )
}

function InfoCell({ label, value }: { label: string; value: string }) {
  const text = value || '-'
  return (
    <div className="min-h-[76px] min-w-0 rounded-sm border border-border bg-background px-3 py-2">
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
  if (action === 'group_sync') return '自动分组'
  return action === 'restore' ? '恢复' : '关闭'
}

function schedulerLogTitle(log: SchedulerLog) {
  if (log.action === 'group_sync') return '自动分组变更'
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
