import { useEffect, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, CirclePause, CirclePlay, Database, Gauge, Loader2, Plus, RefreshCcw, RotateCcw, Settings2, ShieldAlert, Trash2 } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { ActionRow, DataTable, EmptyPanel, Field, FormError, InlineMessage, Metric } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { alertError } from '@/lib/feedback'
import { fmtTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { AvailabilityPolicy, GGAPIAffinityCacheStats, GGAPIAffinityRule, GGAPISettings, GGAPISettingsUpdateResult, SchedulerControlPlane, SchedulerControlPlaneChannel } from '@/types'

export function GGAPICollaborationConsole({ configured }: { configured: boolean }) {
  const qc = useQueryClient()
  const [policyTarget, setPolicyTarget] = useState<SchedulerControlPlaneChannel>()
  const control = useQuery({
    queryKey: ['scheduler', 'control-plane'],
    queryFn: () => api<SchedulerControlPlane>('/api/scheduler/control-plane'),
    enabled: configured,
    refetchInterval: 5000,
  })
  const refresh = useMutation({
    mutationFn: () => api('/api/scheduler/traffic/reconcile', { method: 'POST' }),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['scheduler', 'control-plane'] }) },
    onError: alertError,
  })
  const adopt = useMutation({
    mutationFn: (channelID: string) => api(`/api/scheduler/control-plane/${channelID}/adopt`, { method: 'POST' }),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['scheduler'] }) },
    onError: alertError,
  })
  const availabilityAction = useMutation({
    mutationFn: ({ cardID, action }: { cardID: string; action: string }) =>
      api(`/api/scheduler/availability/${cardID}/action`, { method: 'POST', body: JSON.stringify({ action, minutes: 30 }) }),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['scheduler'] }) },
    onError: alertError,
  })
  const baseline = useMutation({
    mutationFn: (channelID: string) => api(`/api/scheduler/traffic/${channelID}/baseline`, { method: 'POST' }),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['scheduler', 'control-plane'] }) },
    onError: alertError,
  })
  const data = control.data
  const rows = data?.channels ?? []
  const external = rows.filter((row) => row.external_takeover).length
  const closed = rows.filter((row) => row.remote_status !== 1).length
  const cleanup = rows.filter((row) => row.affinity_cleanup_pending).length
  const sessionFailures = rows.reduce((sum, row) => sum + row.session_failures, 0)
  return (
    <section className="grid min-w-0 gap-4">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">GGAPI 协作控制台</h2>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Activity className="size-3.5" />
            <span>{trafficModeText(data?.traffic.mode)}</span>
            {data?.traffic.frozen && <Badge variant="destructive">冻结</Badge>}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {configured && <GGAPISettingsDialog />}
          <Button variant="outline" size="icon" title="立即协调" onClick={() => refresh.mutate()} disabled={refresh.isPending || !configured}>
            <RefreshCcw className={cn('size-4', refresh.isPending && 'animate-spin')} />
            <span className="sr-only">立即协调</span>
          </Button>
        </div>
      </div>
      {!configured && <InlineMessage message="请先完成 GGAPI 连接配置" tone="warning" />}
      {configured && <>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <Metric label="遥测延迟" value={data ? `${data.traffic.lag_seconds}s` : '-'} accent={data?.traffic.frozen ? 'danger' : 'success'} />
          <Metric label="受控渠道" value={rows.length} />
          <Metric label="远端关闭" value={closed} accent={closed ? 'danger' : undefined} />
          <Metric label="外部接管" value={external} accent={external ? 'danger' : undefined} />
          <Metric label="亲和请求失败（5 分钟）" value={sessionFailures} />
        </div>
        {cleanup > 0 && <InlineMessage message={`${cleanup} 个渠道的亲和缓存清理正在重试`} tone="warning" />}
        <FormError error={control.error} />
        {control.isLoading && <EmptyPanel text="加载中..." />}
        {!control.isLoading && rows.length === 0 && <EmptyPanel text="暂无 GGAPI 渠道" />}
        {rows.length > 0 && (
          <DataTable
            minWidthClass="min-w-[1320px]"
            head={<tr><Header>渠道</Header><Header>远端状态</Header><Header>控制所有者</Header><Header>关闭原因</Header><Header>恢复后流量</Header><Header>1 分钟质量</Header><Header>优先级 / 权重</Header><Header>缓存清理</Header><Header align="right">操作</Header></tr>}
          >
            {rows.map((row) => {
              const availability = row.availability
              const traffic = row.traffic
              const manualOff = availability?.state === 'manual_off'
              const forceOn = availability?.override === 'force_enable'
              return (
                <tr key={row.channel_id} className="border-t border-border first:border-t-0">
                  <Cell><div className="font-medium">{row.channel_name || row.channel_id}</div><div className="text-xs text-muted-foreground">#{row.channel_id}</div></Cell>
                  <Cell><Badge variant={statusVariant(row.remote_status)}>{statusText(row.remote_status)}</Badge></Cell>
                  <Cell><OwnerBadge row={row} /></Cell>
                  <Cell><div className="max-w-64 truncate text-xs" title={row.close_reason || availabilityReason(row)}>{row.close_reason || availabilityReason(row)}</div></Cell>
                  <Cell><div>{row.new_traffic_requests} 请求</div><div className="text-xs text-muted-foreground">起点 {fmtTime(row.traffic_since)}</div></Cell>
                  <Cell>{traffic?.window_1m ? <div><span>{Math.round(traffic.window_1m.failure_rate * 100)}%</span><span className="ml-2 text-xs text-muted-foreground">P95 {traffic.window_1m.p95_ttft_ms || '-'}ms</span></div> : '-'}</Cell>
                  <Cell>{row.remote_priority} / {row.remote_weight}</Cell>
                  <Cell>{row.affinity_cleanup_pending ? <div><Badge variant="destructive">待重试</Badge><div className="mt-1 max-w-48 truncate text-xs text-muted-foreground" title={row.affinity_cleanup_error}>{fmtTime(row.affinity_cleanup_retry_at)}</div></div> : <Badge variant="outline">已完成</Badge>}</Cell>
                  <Cell><div className="flex justify-end gap-1">
                    {row.external_takeover && <IconButton title="重新接管" icon={RotateCcw} onClick={() => adopt.mutate(row.channel_id)} disabled={adopt.isPending} />}
                    {availability?.card_id && (manualOff
                      ? <IconButton title="解除手动关闭" icon={CirclePlay} onClick={() => availabilityAction.mutate({ cardID: availability.card_id, action: 'release_hold' })} disabled={availabilityAction.isPending || row.external_takeover} />
                      : <IconButton title="手动关闭" icon={CirclePause} onClick={() => availabilityAction.mutate({ cardID: availability.card_id, action: 'hold_off' })} disabled={availabilityAction.isPending || row.external_takeover || row.remote_status === 3} />)}
                    {availability?.card_id && !forceOn && <IconButton title="限时启用" icon={ShieldAlert} onClick={() => availabilityAction.mutate({ cardID: availability.card_id, action: 'force_enable' })} disabled={availabilityAction.isPending || row.external_takeover || row.remote_status === 3} />}
                    {availability?.upstream_id && <IconButton title="余额策略" icon={Settings2} onClick={() => setPolicyTarget(row)} />}
                    {traffic?.managed && <IconButton title="采纳流量基准" icon={Gauge} onClick={() => baseline.mutate(row.channel_id)} disabled={baseline.isPending} />}
                  </div></Cell>
                </tr>
              )
            })}
          </DataTable>
        )}
      </>}
      <AvailabilityPolicyDialog target={policyTarget} onClose={() => setPolicyTarget(undefined)} />
    </section>
  )
}

function GGAPISettingsDialog() {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<GGAPISettings>()
  const [ruleMode, setRuleMode] = useState<'visual' | 'json'>('visual')
  const [ruleJSON, setRuleJSON] = useState('[]')
  const [message, setMessage] = useState('')
  const [messageError, setMessageError] = useState(false)
  const settings = useQuery({ queryKey: ['scheduler', 'ggapi', 'settings'], queryFn: () => api<GGAPISettings>('/api/scheduler/ggapi/settings'), enabled: open })
  const cache = useQuery({ queryKey: ['scheduler', 'ggapi', 'affinity-cache'], queryFn: () => api<GGAPIAffinityCacheStats>('/api/scheduler/ggapi/affinity-cache'), enabled: open })
  useEffect(() => {
    if (!settings.data) return
    setDraft(structuredClone(settings.data))
    setRuleJSON(JSON.stringify(settings.data.affinity.rules ?? [], null, 2))
    setMessage('')
    setMessageError(false)
  }, [settings.data])
  const save = useMutation({
    mutationFn: () => {
      if (!draft) throw new Error('GGAPI 设置未加载')
      const payload = structuredClone(draft)
      if (ruleMode === 'json') payload.affinity.rules = JSON.parse(ruleJSON) as GGAPIAffinityRule[]
      return api<GGAPISettingsUpdateResult>('/api/scheduler/ggapi/settings', { method: 'PUT', body: JSON.stringify(payload) })
    },
    onSuccess: async (result) => {
      setDraft(structuredClone(result.settings))
      setRuleJSON(JSON.stringify(result.settings.affinity.rules ?? [], null, 2))
      setMessageError(!result.complete)
      setMessage(result.complete ? `已保存 ${result.applied.length} 项` : `部分生效：${result.failed_key || result.error || '远端拒绝写入'}`)
      await qc.setQueryData(['scheduler', 'ggapi', 'settings'], result.settings)
    },
    onError: (error) => { setMessageError(true); setMessage(error instanceof Error ? error.message : '保存失败') },
  })
  const clearCache = useMutation({
    mutationFn: (rule?: string) => api(`/api/scheduler/ggapi/affinity-cache?${rule ? `rule_name=${encodeURIComponent(rule)}` : 'all=true'}`, { method: 'DELETE' }),
    onSuccess: async () => { await cache.refetch() },
    onError: alertError,
  })
  const rules = Array.isArray(draft?.affinity.rules) ? draft.affinity.rules : []
  const updateRule = (index: number, patch: Partial<GGAPIAffinityRule>) => {
    if (!draft) return
    const next = rules.map((rule, i) => i === index ? { ...rule, ...patch } : rule)
    setDraft({ ...draft, affinity: { ...draft.affinity, rules: next } })
    setRuleJSON(JSON.stringify(next, null, 2))
  }
  const updateSource = (ruleIndex: number, sourceIndex: number, patch: Partial<GGAPIAffinityRule['key_sources'][number]>) => {
    const rule = rules[ruleIndex]
    if (!rule) return
    updateRule(ruleIndex, { key_sources: (rule.key_sources ?? []).map((source, index) => index === sourceIndex ? { ...source, ...patch } : source) })
  }
  const changeRuleMode = (next: 'visual' | 'json') => {
    if (next === ruleMode) return
    if (next === 'visual') {
      try {
        const parsed = JSON.parse(ruleJSON) as unknown
        if (!Array.isArray(parsed)) throw new Error('亲和规则必须是 JSON 数组')
        if (!draft) return
        setDraft({ ...draft, affinity: { ...draft.affinity, rules: parsed as GGAPIAffinityRule[] } })
        setMessage('')
        setMessageError(false)
      } catch (error) {
        setMessageError(true)
        setMessage(error instanceof Error ? error.message : '亲和规则 JSON 格式错误')
        return
      }
    } else {
      setRuleJSON(JSON.stringify(rules, null, 2))
    }
    setRuleMode(next)
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild><Button variant="outline" size="sm"><Settings2 className="size-4" />GGAPI 设置</Button></DialogTrigger>
      <DialogContent className="max-h-[92vh] max-w-5xl overflow-y-auto">
        <DialogTitle>GGAPI 设置</DialogTitle>
        <FormError error={settings.error || cache.error} />
        {!draft && <EmptyPanel text="加载中..." />}
        {draft && <div className="grid gap-7">
          <SettingsBand title="路由可靠性">
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              <Field label="重试次数"><Input type="number" min="0" max="10" value={draft.retry_times} onChange={(e) => setDraft({ ...draft, retry_times: Number(e.target.value) })} /></Field>
              <Field label="自动重试状态码"><Input value={draft.automatic_retry_status_codes} onChange={(e) => setDraft({ ...draft, automatic_retry_status_codes: e.target.value })} /></Field>
              <Field label="自动禁用状态码"><Input value={draft.automatic_disable_status_codes} onChange={(e) => setDraft({ ...draft, automatic_disable_status_codes: e.target.value })} /></Field>
              <Field label="通道测试模式"><Select value={draft.channel_test_mode} onValueChange={(value: GGAPISettings['channel_test_mode']) => setDraft({ ...draft, channel_test_mode: value })}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="scheduled_all">定时全量测试</SelectItem><SelectItem value="passive_recovery">被动恢复</SelectItem></SelectContent></Select></Field>
              <Field label="测试间隔（分钟）"><Input type="number" min="0.1" step="0.1" value={draft.auto_test_channel_minutes} onChange={(e) => setDraft({ ...draft, auto_test_channel_minutes: Number(e.target.value) })} /></Field>
            </div>
            <div className="flex flex-wrap gap-3">
              <Toggle label="定时测试" checked={draft.auto_test_channel_enabled} onChange={(checked) => setDraft({ ...draft, auto_test_channel_enabled: checked })} />
              <Toggle label="自动禁用" checked={draft.automatic_disable_channel_enabled} onChange={(checked) => setDraft({ ...draft, automatic_disable_channel_enabled: checked })} />
              <Toggle label="自动恢复" checked={draft.automatic_enable_channel_enabled} onChange={(checked) => setDraft({ ...draft, automatic_enable_channel_enabled: checked })} />
            </div>
          </SettingsBand>
          <SettingsBand title="渠道亲和性">
            <div className="flex flex-wrap gap-3">
              <Toggle label="启用亲和" checked={draft.affinity.enabled} onChange={(checked) => setDraft({ ...draft, affinity: { ...draft.affinity, enabled: checked } })} />
              <Toggle label="成功后切换" checked={draft.affinity.switch_on_success} onChange={(checked) => setDraft({ ...draft, affinity: { ...draft.affinity, switch_on_success: checked } })} />
              <Toggle label="保留已关闭渠道" checked={draft.affinity.keep_on_channel_disabled} onChange={(checked) => setDraft({ ...draft, affinity: { ...draft.affinity, keep_on_channel_disabled: checked } })} />
            </div>
            <div className="grid gap-4 md:grid-cols-2">
              <Field label="缓存容量"><Input type="number" min="1" value={draft.affinity.max_entries} onChange={(e) => setDraft({ ...draft, affinity: { ...draft.affinity, max_entries: Number(e.target.value) } })} /></Field>
              <Field label="默认 TTL（秒）"><Input type="number" min="1" value={draft.affinity.default_ttl_seconds} onChange={(e) => setDraft({ ...draft, affinity: { ...draft.affinity, default_ttl_seconds: Number(e.target.value) } })} /></Field>
            </div>
            <Segmented value={ruleMode} onChange={changeRuleMode} />
            {ruleMode === 'json' ? (
              <textarea className="min-h-80 w-full resize-y rounded-md border border-input bg-background p-3 font-mono text-sm outline-none focus:border-ring" value={ruleJSON} onChange={(e) => setRuleJSON(e.target.value)} spellCheck={false} />
            ) : (
              <div className="grid gap-3">
                {rules.map((rule, index) => <div key={`${rule.name}-${index}`} className="grid gap-3 rounded-md border border-border p-3 md:grid-cols-[1fr_1fr_1fr_120px_44px]">
                  <Field label="规则名"><Input value={rule.name} onChange={(e) => updateRule(index, { name: e.target.value })} /></Field>
                  <Field label="模型正则"><textarea className="min-h-10 rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:border-ring" value={(rule.model_regex ?? []).join('\n')} onChange={(e) => updateRule(index, { model_regex: splitLines(e.target.value) })} /></Field>
                  <Field label="路径正则"><textarea className="min-h-10 rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:border-ring" value={(rule.path_regex ?? []).join('\n')} onChange={(e) => updateRule(index, { path_regex: splitLines(e.target.value) })} /></Field>
                  <Field label="TTL"><Input type="number" min="0" value={rule.ttl_seconds} onChange={(e) => updateRule(index, { ttl_seconds: Number(e.target.value) })} /></Field>
                  <div className="flex items-end"><Button variant="ghost" size="icon" title="删除规则" onClick={() => { const next = rules.filter((_, i) => i !== index); setDraft({ ...draft, affinity: { ...draft.affinity, rules: next } }); setRuleJSON(JSON.stringify(next, null, 2)) }}><Trash2 className="size-4" /><span className="sr-only">删除规则</span></Button></div>
                  <div className="md:col-span-5 grid gap-2 border-t border-border pt-3">
                    {(rule.key_sources ?? []).map((source, sourceIndex) => <div key={`${source.type}-${sourceIndex}`} className="grid gap-2 sm:grid-cols-[180px_1fr_36px]">
                      <Select value={source.type} onValueChange={(value: GGAPIAffinityRule['key_sources'][number]['type']) => updateSource(index, sourceIndex, { type: value })}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="gjson">gjson</SelectItem><SelectItem value="request_header">request_header</SelectItem><SelectItem value="context_string">context_string</SelectItem><SelectItem value="context_int">context_int</SelectItem></SelectContent></Select>
                      <Input value={source.type === 'gjson' ? source.path ?? '' : source.key ?? ''} onChange={(e) => updateSource(index, sourceIndex, source.type === 'gjson' ? { path: e.target.value } : { key: e.target.value })} />
                      <Button variant="ghost" size="icon" title="删除键来源" disabled={(rule.key_sources?.length ?? 0) <= 1} onClick={() => updateRule(index, { key_sources: (rule.key_sources ?? []).filter((_, i) => i !== sourceIndex) })}><Trash2 className="size-4" /><span className="sr-only">删除键来源</span></Button>
                    </div>)}
                    <Button variant="ghost" size="sm" className="justify-self-start" onClick={() => updateRule(index, { key_sources: [...(rule.key_sources ?? []), { type: 'gjson', path: '' }] })}><Plus className="size-4" />新增键来源</Button>
                    <div className="flex flex-wrap gap-3">
                      <Toggle compact label="失败不重试" checked={rule.skip_retry_on_failure} onChange={(checked) => updateRule(index, { skip_retry_on_failure: checked })} />
                      <Toggle compact label="包含分组" checked={rule.include_using_group} onChange={(checked) => updateRule(index, { include_using_group: checked })} />
                      <Toggle compact label="包含模型" checked={rule.include_model_name} onChange={(checked) => updateRule(index, { include_model_name: checked })} />
                      <Toggle compact label="包含规则名" checked={rule.include_rule_name} onChange={(checked) => updateRule(index, { include_rule_name: checked })} />
                    </div>
                  </div>
                </div>)}
                <Button variant="outline" size="sm" className="justify-self-start" onClick={() => { const next = [...rules, emptyRule(rules.length + 1)]; setDraft({ ...draft, affinity: { ...draft.affinity, rules: next } }); setRuleJSON(JSON.stringify(next, null, 2)) }}><Plus className="size-4" />新增规则</Button>
              </div>
            )}
            <div className="grid gap-3 border-t border-border pt-4 md:grid-cols-[1fr_auto] md:items-center">
              <div className="text-sm"><span className="font-medium">Redis 缓存 {cache.data?.total ?? '-'}</span><span className="ml-3 text-muted-foreground">容量 {cache.data?.cache_capacity ?? '-'} · {cache.data?.cache_algo || '-'}</span></div>
              <Button variant="outline" size="sm" onClick={() => clearCache.mutate(undefined)} disabled={clearCache.isPending}><Database className="size-4" />清空全部</Button>
              {Object.entries(cache.data?.by_rule_name ?? {}).map(([name, count]) => <div key={name} className="md:col-span-2 flex items-center justify-between border-t border-border py-2 text-sm"><span>{name} · {count}</span><Button variant="ghost" size="sm" onClick={() => clearCache.mutate(name)} disabled={clearCache.isPending}><Trash2 className="size-4" />清理</Button></div>)}
            </div>
          </SettingsBand>
          <InlineMessage message={message} tone={messageError ? 'error' : 'success'} />
          <ActionRow><Button onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Settings2 className="size-4" />}保存到 GGAPI</Button></ActionRow>
        </div>}
      </DialogContent>
    </Dialog>
  )
}

function AvailabilityPolicyDialog({ target, onClose }: { target?: SchedulerControlPlaneChannel; onClose: () => void }) {
  const qc = useQueryClient()
  const [draft, setDraft] = useState<AvailabilityPolicy>()
  const upstreamID = target?.availability?.upstream_id ?? ''
  const policy = useQuery({ queryKey: ['availability-policy', upstreamID], queryFn: () => api<AvailabilityPolicy>(`/api/upstreams/${upstreamID}/availability-policy`), enabled: Boolean(upstreamID) })
  useEffect(() => { if (policy.data) setDraft(policy.data) }, [policy.data])
  const save = useMutation({
    mutationFn: () => api(`/api/upstreams/${upstreamID}/availability-policy`, { method: 'PATCH', body: JSON.stringify(draft) }),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['scheduler'] }); onClose() },
    onError: alertError,
  })
  return <Dialog open={Boolean(target)} onOpenChange={(open) => { if (!open) onClose() }}><DialogContent><DialogTitle>余额保护策略</DialogTitle>{draft && <div className="grid gap-4 sm:grid-cols-2">
    <Field label="模式"><Select value={draft.balance_guard_mode} onValueChange={(value: AvailabilityPolicy['balance_guard_mode']) => setDraft({ ...draft, balance_guard_mode: value })}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="observe">观察</SelectItem><SelectItem value="active">自动摘流</SelectItem></SelectContent></Select></Field>
    <Field label="耗尽预警（小时）"><Input type="number" min="0" value={draft.runway_warning_hours} onChange={(e) => setDraft({ ...draft, runway_warning_hours: Number(e.target.value) })} /></Field>
    <Field label="预警线"><Input type="number" min="0" value={draft.low_balance_threshold} onChange={(e) => setDraft({ ...draft, low_balance_threshold: Number(e.target.value) })} /></Field>
    <Field label="关闭线"><Input type="number" min="0" value={draft.balance_close_threshold} onChange={(e) => setDraft({ ...draft, balance_close_threshold: Number(e.target.value) })} /></Field>
    <Field label="恢复线"><Input type="number" min="0" value={draft.balance_recover_threshold} onChange={(e) => setDraft({ ...draft, balance_recover_threshold: Number(e.target.value) })} /></Field>
    <ActionRow className="sm:col-span-2"><Button onClick={() => save.mutate()} disabled={save.isPending}>保存策略</Button></ActionRow>
  </div>}</DialogContent></Dialog>
}

function Header({ children, align }: { children: ReactNode; align?: 'right' }) { return <th className={cn('px-3 py-2 font-medium', align === 'right' && 'text-right')}>{children}</th> }
function Cell({ children }: { children: ReactNode }) { return <td className="px-3 py-2 align-middle">{children}</td> }
function IconButton({ title, icon: Icon, onClick, disabled }: { title: string; icon: LucideIcon; onClick: () => void; disabled?: boolean }) { return <Button variant="ghost" size="icon" title={title} onClick={onClick} disabled={disabled}><Icon className="size-4" /><span className="sr-only">{title}</span></Button> }
function OwnerBadge({ row }: { row: SchedulerControlPlaneChannel }) { const label = row.owner === 'ggapi' ? 'GGAPI' : row.owner === 'external' ? '外部接管' : 'AUM'; return <Badge variant={row.external_takeover ? 'destructive' : row.owner === 'ggapi' ? 'secondary' : 'outline'}>{label}</Badge> }
function statusText(status: number) { return status === 1 ? '启用' : status === 2 ? '手动关闭' : status === 3 ? '自动关闭' : `状态 ${status}` }
function statusVariant(status: number): 'success' | 'destructive' | 'secondary' { return status === 1 ? 'success' : status === 2 ? 'destructive' : 'secondary' }
function trafficModeText(mode?: string) { return mode === 'active' ? '主动控制' : mode === 'observe' ? '仅观察' : '流量控制关闭' }
function availabilityReason(row: SchedulerControlPlaneChannel) { const availability = row.availability; if (!availability) return '-'; if (availability.blockers.length) return availability.blockers.map((item) => item.message || item.kind).join('；'); if (availability.override === 'manual_hold') return 'AUM 手动关闭'; if (availability.override === 'force_enable') return 'AUM 限时启用'; return availability.state || '-' }
function splitLines(value: string) { return value.split('\n').map((item) => item.trim()).filter(Boolean) }
function emptyRule(index: number): GGAPIAffinityRule { return { name: `rule-${index}`, model_regex: ['.*'], path_regex: ['/v1/responses'], key_sources: [{ type: 'gjson', path: 'prompt_cache_key' }], value_regex: '', ttl_seconds: 0, skip_retry_on_failure: false, include_using_group: true, include_model_name: false, include_rule_name: true } }
function SettingsBand({ title, children }: { title: string; children: ReactNode }) { return <section className="grid gap-4 border-t border-border pt-5 first:border-t-0 first:pt-0"><h3 className="text-sm font-semibold">{title}</h3>{children}</section> }
function Segmented({ value, onChange }: { value: 'visual' | 'json'; onChange: (value: 'visual' | 'json') => void }) { return <div className="flex w-fit rounded-md border border-border bg-background"><button className={cn('h-9 px-3 text-sm', value === 'visual' && 'bg-secondary')} onClick={() => onChange('visual')}>可视化</button><button className={cn('h-9 border-l border-border px-3 text-sm', value === 'json' && 'bg-secondary')} onClick={() => onChange('json')}>JSON</button></div> }
function Toggle({ label, checked, onChange, compact }: { label: string; checked: boolean; onChange: (checked: boolean) => void; compact?: boolean }) { return <button type="button" role="switch" aria-checked={checked} className={cn('inline-flex items-center gap-2 whitespace-nowrap text-sm', compact ? 'h-7' : 'h-9 rounded-md border border-border px-3')} onClick={() => onChange(!checked)}><span className={cn('relative h-5 w-9 shrink-0 rounded-full transition-colors', checked ? 'bg-success' : 'bg-muted-foreground/30')}><span className={cn('absolute top-0.5 size-4 rounded-full bg-white transition-transform', checked ? 'translate-x-[18px]' : 'translate-x-0.5')} /></span>{label}</button> }
