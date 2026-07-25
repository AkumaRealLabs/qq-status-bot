import { useEffect, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Database, Loader2, Plus, RefreshCcw, RotateCcw, Settings2, Trash2 } from 'lucide-react'
import { ActionRow, DataTable, EmptyPanel, FeedbackBanner, Field, FormError, IconAction, InlineMessage, Metric, SaveButton } from '@/components/common'
import { Page, ShellLoading } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { alertError, closeAfterSave, confirmDelete, secretPlaceholder, useFeedback } from '@/lib/feedback'
import { fmtTime, num } from '@/lib/format'
import { cn } from '@/lib/utils'
import type {
  AxonHubConfig,
  CostBindingForm,
  SchedulerApplyResult,
  SchedulerChannel,
  SchedulerConfig,
  SchedulerCostBinding,
  SchedulerLog,
  SchedulerTier,
  UpstreamRow,
} from '@/types'

const none = '__none__'
const defaultTiers: SchedulerTier[] = [
  { tag: 'gpt_low', group: 'gpt_low', price_min: 0, price_max: 0.1, sale_price: 0.1 },
  { tag: 'gpt_stable', group: 'gpt_stable', price_min: 0, price_max: 0.25, sale_price: 0.25 },
]

const emptyBinding: CostBindingForm = {
  name: '',
  source_type: 'upstream_key',
  upstream_id: '',
  key_id: '',
  manual_cost_ratio: '',
  scheduler_channel_id: '',
  scheduler_channel_name: '',
  axonhub_channel_id: '',
  axonhub_channel_name: '',
  enabled: true,
}

export function CostsPage() {
  const qc = useQueryClient()
  const syncFeedback = useFeedback('成本同步完成')
  const bindings = useQuery({ queryKey: ['cost-bindings'], queryFn: () => api<SchedulerCostBinding[]>('/api/cost-bindings') })
  const config = useQuery({ queryKey: ['scheduler', 'config'], queryFn: () => api<SchedulerConfig>('/api/scheduler/config') })
  const axonConfig = useQuery({ queryKey: ['scheduler', 'axonhub', 'config'], queryFn: () => api<AxonHubConfig>('/api/scheduler/axonhub/config') })
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamRow[]>('/api/upstreams') })
  const logs = useQuery({ queryKey: ['scheduler', 'logs'], queryFn: () => api<SchedulerLog[]>('/api/scheduler/logs?limit=20') })
  const ggapiConfigured = Boolean(config.data?.scheduler_base_url && config.data.scheduler_user_id && config.data.scheduler_access_token_set)
  const axonConfigured = Boolean(axonConfig.data?.base_url && axonConfig.data.admin_email && axonConfig.data.admin_password_set)
  const ggapiChannels = useQuery({
    queryKey: ['cost-bindings', 'channels', 'ggapi'],
    queryFn: () => api<SchedulerChannel[]>('/api/cost-bindings/channels?provider=ggapi'),
    enabled: ggapiConfigured,
  })
  const axonChannels = useQuery({
    queryKey: ['cost-bindings', 'channels', 'axonhub'],
    queryFn: () => api<SchedulerChannel[]>('/api/cost-bindings/channels?provider=axonhub'),
    enabled: axonConfigured,
  })
  const sync = useMutation({
    mutationFn: () => api<SchedulerApplyResult>('/api/scheduler/groups/apply', { method: 'POST', body: '{}' }),
    onMutate: () => syncFeedback.pending('同步中...'),
    onSuccess: async (result) => {
      syncFeedback.success(`成本同步完成：更新 ${result.updated} 个，未变更 ${result.unchanged} 个，跳过 ${result.skipped} 个`)
      await Promise.all([
        qc.invalidateQueries({ queryKey: ['cost-bindings'] }),
        qc.invalidateQueries({ queryKey: ['scheduler', 'logs'] }),
        qc.invalidateQueries({ queryKey: ['cost-bindings', 'channels'] }),
      ])
    },
    onError: syncFeedback.fail,
  })
  const rows = bindings.data ?? []
  const ready = rows.filter((row) => row.enabled && row.cost_available).length
  const missing = rows.filter((row) => row.enabled && !row.cost_available).length
  const takeover = rows.filter((row) => row.ggapi_external_takeover || row.axonhub_external_takeover).length
  const refreshing = bindings.isFetching || config.isFetching || axonConfig.isFetching || upstreams.isFetching || logs.isFetching || ggapiChannels.isFetching || axonChannels.isFetching
  const refreshAll = () => {
    void bindings.refetch()
    void config.refetch()
    void axonConfig.refetch()
    void upstreams.refetch()
    void logs.refetch()
    if (ggapiConfigured) void ggapiChannels.refetch()
    if (axonConfigured) void axonChannels.refetch()
  }
  if (!config.data || !axonConfig.data) return <ShellLoading />
  return (
    <Page
      title="成本管理"
      description="成本来源、渠道绑定、售价档位与成本字段同步"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          <Button variant="outline" size="sm" onClick={refreshAll} disabled={refreshing}>
            <RefreshCcw className={cn('size-4', refreshing && 'animate-spin')} />
            刷新
          </Button>
          <Button size="sm" onClick={() => sync.mutate()} disabled={sync.isPending}>
            {sync.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCcw className="size-4" />}
            立即同步
          </Button>
          <CostBindingDialog upstreams={upstreams.data ?? []} ggapiChannels={ggapiChannels.data ?? []} axonChannels={axonChannels.data ?? []} bindings={rows} />
        </div>
      }
    >
      <FeedbackBanner message={syncFeedback.message} error={sync.isError} success="成本同步完成" />
      <FormError error={bindings.error || config.error || axonConfig.error || upstreams.error} />
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Metric label="成本绑定" value={rows.length} />
        <Metric label="可同步" value={ready} accent={ready > 0 ? 'success' : undefined} />
        <Metric label="成本缺失" value={missing} accent={missing > 0 ? 'danger' : undefined} />
        <Metric label="外部接管" value={takeover} accent={takeover > 0 ? 'danger' : undefined} />
      </div>

      <section className="grid min-w-0 gap-3">
        <div>
          <h2 className="text-base font-medium text-foreground">成本绑定</h2>
          <p className="mt-1 text-sm text-muted-foreground">同步只修改 GGAPI 分组与优先级，或 AxonHub 托管标签与权重，不修改渠道启停状态。</p>
        </div>
        {bindings.isLoading && <EmptyPanel text="加载中..." />}
        {!bindings.isLoading && rows.length === 0 && <EmptyPanel text="暂无成本绑定" />}
        {rows.length > 0 && (
          <>
            <div className="hidden md:block">
              <DataTable minWidthClass="min-w-[1120px]" head={<tr><Head>名称</Head><Head>成本来源</Head><Head>Key 倍率</Head><Head>余额倍率</Head><Head>最终成本</Head><Head>GGAPI 渠道</Head><Head>AxonHub 渠道</Head><Head>状态</Head><Head align="right">操作</Head></tr>}>
                {rows.map((row) => <CostBindingTableRow key={row.id} row={row} upstreams={upstreams.data ?? []} ggapiChannels={ggapiChannels.data ?? []} axonChannels={axonChannels.data ?? []} bindings={rows} />)}
              </DataTable>
            </div>
            <div className="grid gap-3 md:hidden">
              {rows.map((row) => <CostBindingCard key={row.id} row={row} upstreams={upstreams.data ?? []} ggapiChannels={ggapiChannels.data ?? []} axonChannels={axonChannels.data ?? []} bindings={rows} />)}
            </div>
          </>
        )}
      </section>

      <CostSettings config={config.data} axonConfig={axonConfig.data} ggapiConfigured={ggapiConfigured} axonConfigured={axonConfigured} />
      <CostSyncLogs rows={logs.data ?? []} loading={logs.isLoading} />
    </Page>
  )
}

function CostBindingTableRow(props: BindingActionsProps) {
  const { row } = props
  return (
    <tr className="border-t border-border align-top">
      <Cell><div className="font-medium text-foreground">{row.name}</div><div className="mt-1 text-xs text-muted-foreground">{row.enabled ? '已启用' : '已停用'}</div></Cell>
      <Cell><SourceText row={row} /></Cell>
      <Cell>{row.source_type === 'upstream_key' ? row.key_group_ratio || '-' : '-'}</Cell>
      <Cell>{row.source_type === 'upstream_key' ? num(row.balance_rate) : '-'}</Cell>
      <Cell><CostValue row={row} /></Cell>
      <Cell><ChannelValue provider="ggapi" row={row} /></Cell>
      <Cell><ChannelValue provider="axonhub" row={row} /></Cell>
      <Cell><BindingState row={row} /></Cell>
      <Cell><BindingActions {...props} /></Cell>
    </tr>
  )
}

function CostBindingCard(props: BindingActionsProps) {
  const { row } = props
  return (
    <Card className={cn('bg-card', row.enabled && !row.cost_available && 'border-destructive/40')}>
      <CardHeader>
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0"><CardTitle className="break-words">{row.name}</CardTitle><CardDescription><SourceText row={row} /></CardDescription></div>
          <Badge variant={row.enabled ? 'success' : 'secondary'}>{row.enabled ? '启用' : '停用'}</Badge>
        </div>
      </CardHeader>
      <CardContent className="grid gap-3">
        <div className="grid grid-cols-2 gap-2 text-sm">
          <Value label="Key 倍率" value={row.source_type === 'upstream_key' ? row.key_group_ratio || '-' : '-'} />
          <Value label="balance_rate" value={row.source_type === 'upstream_key' ? num(row.balance_rate) : '-'} />
          <Value label="最终成本" value={row.cost_available ? num(row.effective_cost) : '-'} />
          <Value label="成本状态" value={row.missing_reason || '可用'} danger={Boolean(row.missing_reason)} />
        </div>
        <div className="grid gap-2 text-sm"><ChannelValue provider="ggapi" row={row} /><ChannelValue provider="axonhub" row={row} /></div>
        <BindingActions {...props} />
      </CardContent>
    </Card>
  )
}

type BindingActionsProps = {
  row: SchedulerCostBinding
  upstreams: UpstreamRow[]
  ggapiChannels: SchedulerChannel[]
  axonChannels: SchedulerChannel[]
  bindings: SchedulerCostBinding[]
}

function BindingActions(props: BindingActionsProps) {
  const { row } = props
  const qc = useQueryClient()
  const remove = useMutation({
    mutationFn: () => api(`/api/cost-bindings/${row.id}`, { method: 'DELETE' }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cost-bindings'] }),
    onError: alertError,
  })
  const adopt = useMutation({
    mutationFn: (provider: 'ggapi' | 'axonhub') => api(`/api/cost-bindings/${row.id}/adopt`, { method: 'POST', body: JSON.stringify({ provider }) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cost-bindings'] }),
    onError: alertError,
  })
  return (
    <div className="flex flex-wrap justify-end gap-1.5">
      {row.ggapi_external_takeover && <Button variant="outline" size="sm" title="重新接管 GGAPI 成本字段" onClick={() => adopt.mutate('ggapi')} disabled={adopt.isPending}><RotateCcw className="size-4" />GGAPI</Button>}
      {row.axonhub_external_takeover && <Button variant="outline" size="sm" title="重新接管 AxonHub 成本字段" onClick={() => adopt.mutate('axonhub')} disabled={adopt.isPending}><RotateCcw className="size-4" />AxonHub</Button>}
      <CostBindingDialog {...props} binding={row} />
      <IconAction title="删除成本绑定" icon={Trash2} danger pending={remove.isPending} onClick={() => confirmDelete(row.name) && remove.mutate()} />
    </div>
  )
}

function CostBindingDialog({ binding, upstreams, ggapiChannels, axonChannels, bindings }: Omit<BindingActionsProps, 'row'> & { binding?: SchedulerCostBinding }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<CostBindingForm>(() => bindingForm(binding))
  const fb = useFeedback()
  useEffect(() => {
    if (open) setForm(bindingForm(binding))
  }, [binding, open])
  const selectedUpstream = upstreams.find((row) => row.upstream.id === form.upstream_id)
  const keys = selectedUpstream?.keys ?? []
  const save = useMutation({
    mutationFn: () => api<SchedulerCostBinding>(binding ? `/api/cost-bindings/${binding.id}` : '/api/cost-bindings', {
      method: binding ? 'PATCH' : 'POST',
      body: JSON.stringify(form),
    }),
    onMutate: () => fb.pending(),
    onSuccess: async () => {
      fb.success()
      await qc.invalidateQueries({ queryKey: ['cost-bindings'] })
      closeAfterSave(setOpen)
    },
    onError: fb.fail,
  })
  const setChannel = (provider: 'ggapi' | 'axonhub', id: string) => {
    const list = provider === 'ggapi' ? ggapiChannels : axonChannels
    const channel = list.find((item) => item.id === id)
    if (provider === 'ggapi') setForm({ ...form, scheduler_channel_id: id === none ? '' : id, scheduler_channel_name: channel?.name ?? '' })
    else setForm({ ...form, axonhub_channel_id: id === none ? '' : id, axonhub_channel_name: channel?.name ?? '' })
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {binding
          ? <Button variant="outline" size="icon" title="编辑成本绑定"><Settings2 className="size-4" /><span className="sr-only">编辑成本绑定</span></Button>
          : <Button variant="outline" size="sm"><Plus className="size-4" />新增绑定</Button>}
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogTitle>{binding ? '编辑成本绑定' : '新增成本绑定'}</DialogTitle>
        <div className="grid gap-4">
          <Field label="名称"><Input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field>
          <Field label="成本来源">
            <div className="grid grid-cols-2 overflow-hidden rounded-md border border-border">
              <button className={cn('h-10 text-sm', form.source_type === 'upstream_key' ? 'bg-primary text-primary-foreground' : 'bg-background hover:bg-card')} onClick={() => setForm({ ...form, source_type: 'upstream_key', manual_cost_ratio: '' })}>上游 Key</button>
              <button className={cn('h-10 border-l border-border text-sm', form.source_type === 'manual' ? 'bg-primary text-primary-foreground' : 'bg-background hover:bg-card')} onClick={() => setForm({ ...form, source_type: 'manual', upstream_id: '', key_id: '' })}>手动倍率</button>
            </div>
          </Field>
          {form.source_type === 'upstream_key' ? (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="上游">
                <Select value={form.upstream_id || none} onValueChange={(value) => setForm({ ...form, upstream_id: value === none ? '' : value, key_id: '' })}>
                  <SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value={none}>请选择上游</SelectItem>{upstreams.map((row) => <SelectItem key={row.upstream.id} value={row.upstream.id}>{row.upstream.name}</SelectItem>)}</SelectContent>
                </Select>
              </Field>
              <Field label="Key">
                <Select value={form.key_id || none} onValueChange={(value) => setForm({ ...form, key_id: value === none ? '' : value })} disabled={!form.upstream_id}>
                  <SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value={none}>请选择 Key</SelectItem>{keys.map((key) => <SelectItem key={key.id} value={key.id}>{key.name}{key.group_ratio ? ` · ${key.group_ratio}x` : ''}</SelectItem>)}</SelectContent>
                </Select>
              </Field>
            </div>
          ) : <Field label="手动成本倍率"><Input inputMode="decimal" value={form.manual_cost_ratio ?? ''} placeholder="例如 0.08" onChange={(event) => setForm({ ...form, manual_cost_ratio: event.target.value })} /></Field>}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="GGAPI 渠道"><ChannelSelect provider="ggapi" value={form.scheduler_channel_id ?? ''} currentName={form.scheduler_channel_name} channels={ggapiChannels} bindings={bindings} bindingID={binding?.id} onChange={(value) => setChannel('ggapi', value)} /></Field>
            <Field label="AxonHub 渠道"><ChannelSelect provider="axonhub" value={form.axonhub_channel_id ?? ''} currentName={form.axonhub_channel_name} channels={axonChannels} bindings={bindings} bindingID={binding?.id} onChange={(value) => setChannel('axonhub', value)} /></Field>
          </div>
          <label className="flex items-center gap-2 text-sm font-medium text-foreground"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} />启用成本同步</label>
          <FeedbackBanner message={fb.message} error={save.isError} />
          <ActionRow><SaveButton onClick={() => save.mutate()} pending={save.isPending} message={fb.message} /></ActionRow>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ChannelSelect({ provider, value, currentName, channels, bindings, bindingID, onChange }: { provider: 'ggapi' | 'axonhub'; value: string; currentName?: string; channels: SchedulerChannel[]; bindings: SchedulerCostBinding[]; bindingID?: string; onChange: (value: string) => void }) {
  const field = provider === 'ggapi' ? 'scheduler_channel_id' : 'axonhub_channel_id'
  const used = new Set(bindings.filter((item) => item.id !== bindingID).map((item) => item[field]).filter(Boolean))
  const options = channels.filter((channel) => !used.has(channel.id))
  const missingCurrent = value && !options.some((channel) => channel.id === value)
  return (
    <Select value={value || none} onValueChange={onChange}>
      <SelectTrigger><SelectValue /></SelectTrigger>
      <SelectContent>
        <SelectItem value={none}>不绑定</SelectItem>
        {missingCurrent && <SelectItem value={value}>{currentName || value}（未读取）</SelectItem>}
        {options.map((channel) => <SelectItem key={channel.id} value={channel.id}>{channel.name || channel.id}</SelectItem>)}
      </SelectContent>
    </Select>
  )
}

function CostSettings({ config, axonConfig, ggapiConfigured, axonConfigured }: { config: SchedulerConfig; axonConfig: AxonHubConfig; ggapiConfigured: boolean; axonConfigured: boolean }) {
  const qc = useQueryClient()
  const [configDraft, setConfigDraft] = useState(config)
  const [axonDraft, setAxonDraft] = useState(axonConfig)
  const cfgFeedback = useFeedback()
  const axonFeedback = useFeedback('已保存|连接正常')
  useEffect(() => setConfigDraft(config), [config])
  useEffect(() => setAxonDraft(axonConfig), [axonConfig])
  const tiers = Array.isArray(configDraft.scheduler_tiers) ? configDraft.scheduler_tiers : defaultTiers
  const saveConfig = useMutation({
    mutationFn: () => api<SchedulerConfig>('/api/scheduler/config', { method: 'PATCH', body: JSON.stringify({ ...configDraft, scheduler_tiers: tiers }) }),
    onMutate: () => cfgFeedback.pending(),
    onSuccess: async (out) => { cfgFeedback.success(); setConfigDraft(out); await qc.setQueryData(['scheduler', 'config'], out) },
    onError: cfgFeedback.fail,
  })
  const saveAxon = useMutation({
    mutationFn: () => api<AxonHubConfig>('/api/scheduler/axonhub/config', { method: 'PATCH', body: JSON.stringify(axonDraft) }),
    onMutate: () => axonFeedback.pending(),
    onSuccess: async (out) => { axonFeedback.success(); setAxonDraft(out); await qc.setQueryData(['scheduler', 'axonhub', 'config'], out); void qc.invalidateQueries({ queryKey: ['cost-bindings', 'channels', 'axonhub'] }) },
    onError: axonFeedback.fail,
  })
  const testAxon = useMutation({
    mutationFn: () => api('/api/scheduler/axonhub/test', { method: 'POST' }),
    onMutate: () => axonFeedback.pending('检查中...'),
    onSuccess: () => axonFeedback.success('连接正常'),
    onError: axonFeedback.fail,
  })
  const switchProvider = useMutation({
    mutationFn: (provider: 'ggapi' | 'axonhub') => api<SchedulerConfig>('/api/scheduler/provider/switch', { method: 'POST', body: JSON.stringify({ provider }) }),
    onSuccess: async (out) => { setConfigDraft(out); await qc.setQueryData(['scheduler', 'config'], out); void qc.invalidateQueries({ queryKey: ['scheduler', 'logs'] }) },
    onError: alertError,
  })
  const updateTier = (index: number, patch: Partial<SchedulerTier>) => setConfigDraft({ ...configDraft, scheduler_tiers: tiers.map((tier, i) => i === index ? { ...tier, ...patch } : tier) })
  return (
    <section className="grid min-w-0 gap-3">
      <div>
        <h2 className="text-base font-medium text-foreground">同步配置</h2>
        <p className="mt-1 text-sm text-muted-foreground">当前仅对选中的 provider 自动同步成本字段。</p>
      </div>
      <div className="grid grid-cols-2 overflow-hidden rounded-md border border-border sm:max-w-sm">
        <button className={cn('h-10 text-sm', config.scheduler_provider === 'ggapi' ? 'bg-primary text-primary-foreground' : 'bg-background hover:bg-card')} disabled={switchProvider.isPending || !ggapiConfigured} onClick={() => switchProvider.mutate('ggapi')}>GGAPI</button>
        <button className={cn('h-10 border-l border-border text-sm', config.scheduler_provider === 'axonhub' ? 'bg-primary text-primary-foreground' : 'bg-background hover:bg-card')} disabled={switchProvider.isPending || !axonConfigured} onClick={() => switchProvider.mutate('axonhub')}>AxonHub</button>
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="bg-card">
          <CardHeader><CardTitle>GGAPI 与售价档位</CardTitle><CardDescription>连接、调度分组、成本区间和利润核算售价</CardDescription></CardHeader>
          <CardContent className="grid gap-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Base URL"><Input value={configDraft.scheduler_base_url} onChange={(event) => setConfigDraft({ ...configDraft, scheduler_base_url: event.target.value })} /></Field>
              <Field label="管理员用户 ID"><Input value={configDraft.scheduler_user_id} onChange={(event) => setConfigDraft({ ...configDraft, scheduler_user_id: event.target.value })} /></Field>
            </div>
            <Field label="访问 Token"><Input type="password" value={configDraft.scheduler_access_token ?? ''} placeholder={secretPlaceholder(configDraft.scheduler_access_token_set)} onChange={(event) => setConfigDraft({ ...configDraft, scheduler_access_token: event.target.value })} /></Field>
            <Field label="未分配分组"><Input value={configDraft.scheduler_unassigned_group} onChange={(event) => setConfigDraft({ ...configDraft, scheduler_unassigned_group: event.target.value })} /></Field>
            <div className="grid gap-2">
              <div className="text-sm font-medium text-foreground">调度层级与售价</div>
              {tiers.map((tier, index) => (
                <div key={index} className="grid gap-2 border-t border-border pt-3 first:border-t-0 first:pt-0 sm:grid-cols-5">
                  <Input aria-label="层级名称" placeholder="层级" value={tier.tag} onChange={(event) => updateTier(index, { tag: event.target.value })} />
                  <Input aria-label="调度器分组" placeholder="分组" value={tier.group} onChange={(event) => updateTier(index, { group: event.target.value })} />
                  <Input aria-label="最低成本" type="number" min="0" step="0.001" value={tier.price_min} onChange={(event) => updateTier(index, { price_min: Number(event.target.value) })} />
                  <Input aria-label="最高成本" type="number" min="0" step="0.001" value={tier.price_max} onChange={(event) => updateTier(index, { price_max: Number(event.target.value) })} />
                  <div className="flex gap-1"><Input aria-label="售价" type="number" min="0" step="0.001" value={tier.sale_price} onChange={(event) => updateTier(index, { sale_price: Number(event.target.value) })} /><Button variant="ghost" size="icon" title="删除层级" onClick={() => setConfigDraft({ ...configDraft, scheduler_tiers: tiers.filter((_, i) => i !== index) })}><Trash2 className="size-4" /><span className="sr-only">删除层级</span></Button></div>
                </div>
              ))}
              <Button variant="outline" size="sm" className="justify-self-start" onClick={() => setConfigDraft({ ...configDraft, scheduler_tiers: [...tiers, { tag: `tier_${tiers.length + 1}`, group: '', price_min: 0, price_max: 0, sale_price: 0 }] })}><Plus className="size-4" />新增层级</Button>
            </div>
            <FeedbackBanner message={cfgFeedback.message} error={saveConfig.isError} />
            <SaveButton onClick={() => saveConfig.mutate()} pending={saveConfig.isPending} message={cfgFeedback.message} label="保存 GGAPI 与售价配置" />
          </CardContent>
        </Card>

        <Card className="bg-card">
          <CardHeader><CardTitle>AxonHub</CardTitle><CardDescription>成本同步使用 payg_low / payg_stable 托管标签与 orderingWeight</CardDescription></CardHeader>
          <CardContent className="grid gap-4">
            <Field label="Base URL"><Input value={axonDraft.base_url} onChange={(event) => setAxonDraft({ ...axonDraft, base_url: event.target.value })} /></Field>
            <Field label="管理员邮箱"><Input type="email" value={axonDraft.admin_email} onChange={(event) => setAxonDraft({ ...axonDraft, admin_email: event.target.value })} /></Field>
            <Field label="管理员密码"><Input type="password" value={axonDraft.admin_password ?? ''} placeholder={secretPlaceholder(axonDraft.admin_password_set)} onChange={(event) => setAxonDraft({ ...axonDraft, admin_password: event.target.value })} /></Field>
            <label className="flex items-center gap-2 text-sm font-medium text-foreground"><input type="checkbox" checked={axonDraft.control_mode === 'active'} onChange={(event) => setAxonDraft({ ...axonDraft, control_mode: event.target.checked ? 'active' : 'off' })} />启用 AxonHub 成本同步</label>
            <InlineMessage message="AxonHub 渠道状态由 AxonHub 自身与 Uptime Kuma 管理，AUM 不会启用或关闭渠道。" />
            <FeedbackBanner message={axonFeedback.message} error={saveAxon.isError || testAxon.isError} success={['已保存', '连接正常']} />
            <div className="flex flex-wrap gap-2">
              <SaveButton onClick={() => saveAxon.mutate()} pending={saveAxon.isPending} message={axonFeedback.message} label="保存 AxonHub 配置" />
              <Button variant="outline" onClick={() => testAxon.mutate()} disabled={testAxon.isPending}>{testAxon.isPending ? <Loader2 className="size-4 animate-spin" /> : <Database className="size-4" />}检查连接</Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </section>
  )
}

function CostSyncLogs({ rows, loading }: { rows: SchedulerLog[]; loading: boolean }) {
  return (
    <section className="grid min-w-0 gap-3">
      <div><h2 className="text-base font-medium text-foreground">成本同步日志</h2></div>
      {loading && <EmptyPanel text="加载中..." />}
      {!loading && rows.length === 0 && <EmptyPanel text="暂无同步日志" />}
      {rows.length > 0 && <DataTable minWidthClass="min-w-[720px]" head={<tr><Head>时间</Head><Head>Provider</Head><Head>结果</Head><Head>说明</Head></tr>}>
        {rows.map((row) => <tr key={row.id} className="border-t border-border"><Cell>{fmtTime(row.created_at)}</Cell><Cell>{row.provider || '-'}</Cell><Cell><Badge variant={row.status === 'success' ? 'success' : row.status === 'error' ? 'destructive' : 'secondary'}>{row.status === 'success' ? '成功' : row.status === 'error' ? '失败' : '跳过'}</Badge></Cell><Cell>{row.message || row.reason || '-'}</Cell></tr>)}
      </DataTable>}
    </section>
  )
}

function bindingForm(binding?: SchedulerCostBinding): CostBindingForm {
  if (!binding) return { ...emptyBinding }
  return {
    name: binding.name,
    source_type: binding.source_type,
    upstream_id: binding.upstream_id ?? '',
    key_id: binding.key_id ?? '',
    manual_cost_ratio: binding.manual_cost_ratio ?? '',
    scheduler_channel_id: binding.scheduler_channel_id ?? '',
    scheduler_channel_name: binding.scheduler_channel_name ?? '',
    axonhub_channel_id: binding.axonhub_channel_id ?? '',
    axonhub_channel_name: binding.axonhub_channel_name ?? '',
    enabled: binding.enabled,
  }
}

function SourceText({ row }: { row: SchedulerCostBinding }) {
  return row.source_type === 'upstream_key'
    ? <div className="max-w-52 break-words"><div>{row.upstream_name || '上游缺失'}</div><div className="text-xs text-muted-foreground">{row.key_name || 'Key 缺失'}</div></div>
    : <div><div>手动倍率</div><div className="text-xs text-muted-foreground">{row.manual_cost_ratio || '未填写'}</div></div>
}

function CostValue({ row }: { row: SchedulerCostBinding }) {
  if (!row.cost_available) return <div className="max-w-48 break-words text-xs text-destructive">{row.missing_reason || '成本不可用'}</div>
  return <span className="font-medium text-foreground">{num(row.effective_cost)}</span>
}

function ChannelValue({ provider, row }: { provider: 'ggapi' | 'axonhub'; row: SchedulerCostBinding }) {
  const id = provider === 'ggapi' ? row.scheduler_channel_id : row.axonhub_channel_id
  const name = provider === 'ggapi' ? row.scheduler_channel_name : row.axonhub_channel_name
  const takeover = provider === 'ggapi' ? row.ggapi_external_takeover : row.axonhub_external_takeover
  const reason = provider === 'ggapi' ? row.ggapi_ownership_reason : row.axonhub_ownership_reason
  return <div className="max-w-52 break-words"><div className="flex flex-wrap items-center gap-1"><span>{name || id || '未绑定'}</span>{takeover && <Badge variant="destructive">外部接管</Badge>}</div>{reason && <div className={cn('mt-1 text-xs', takeover ? 'text-destructive' : 'text-muted-foreground')}>{reason}</div>}</div>
}

function BindingState({ row }: { row: SchedulerCostBinding }) {
  if (!row.enabled) return <Badge variant="secondary">已停用</Badge>
  if (!row.cost_available) return <Badge variant="destructive">成本缺失</Badge>
  if (row.ggapi_external_takeover || row.axonhub_external_takeover) return <Badge variant="destructive">同步暂停</Badge>
  return <Badge variant="success">可同步</Badge>
}

function Value({ label, value, danger }: { label: string; value: string; danger?: boolean }) {
  return <div className="min-w-0 rounded-md border border-border bg-background px-2.5 py-2"><div className="text-xs text-muted-foreground">{label}</div><div className={cn('mt-1 break-words font-medium', danger && 'text-destructive')}>{value}</div></div>
}

function Head({ children, align }: { children: ReactNode; align?: 'right' }) {
  return <th className={cn('whitespace-nowrap px-3 py-2 font-medium', align === 'right' && 'text-right')}>{children}</th>
}

function Cell({ children }: { children: ReactNode }) {
  return <td className="px-3 py-2.5">{children}</td>
}
