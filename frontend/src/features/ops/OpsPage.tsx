import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bell, Check, CheckCircle2, Loader2, Play, RefreshCcw, ShieldCheck } from 'lucide-react'
import { EmptyPanel, Field, FormError, Metric } from '@/components/common'
import { Page, ShellLoading } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage, fmtTime, num } from '@/lib/format'
import { cn } from '@/lib/utils'
import type {
  AuditLog,
  NotificationRules,
  OpsEvent,
  ProfitResponse,
  SelfCheckResponse,
} from '@/types'

const eventLabels: Record<string, string> = {
  probe_failed: '探测失败',
  balance_low: '余额低',
  credential_invalid: '凭据失效',
  balance_query_failed: '额度查询失败',
  scheduler_changed: '调度器变更',
  cliproxy_error: '号池异常',
}

export function EventsPage() {
  return <Page title="事件中心" description="集中处理未读、未确认的运维事件"><EventsTab /></Page>
}

export function AuditPage() {
  return <Page title="审计日志" description="查看管理员操作记录"><AuditTab /></Page>
}

export function NotificationsPage() {
  return <Page title="通知规则" description="配置告警事件和 Telegram 测试"><NotificationsTab /></Page>
}

export function ProfitPage() {
  return <Page title="利润估算" description="收入快照减去余额消耗估算"><ProfitTab /></Page>
}

export function SelfCheckPage() {
  return <Page title="系统自检" description="轻量检查应用、数据库、浏览器和号池管理连通性"><SelfCheckTab /></Page>
}

function EventsTab() {
  const qc = useQueryClient()
  const [state, setState] = useState('unacked')
  const q = useQuery({ queryKey: ['ops', 'events', state], queryFn: () => api<OpsEvent[]>(`/api/ops/events?state=${state}`), refetchInterval: 30000 })
  const markRead = useMutation({ mutationFn: (id: string) => api(`/api/ops/events/${id}/read`, { method: 'POST', body: '{}' }), onSuccess: () => qc.invalidateQueries({ queryKey: ['ops', 'events'] }) })
  const ack = useMutation({ mutationFn: (id: string) => api(`/api/ops/events/${id}/ack`, { method: 'POST', body: '{}' }), onSuccess: () => qc.invalidateQueries({ queryKey: ['ops', 'events'] }) })
  const action = useMutation({
    mutationFn: ({ event, action }: { event: OpsEvent; action: string }) => runEventAction(event, action),
    onSuccess: async (_, vars) => {
      await api(`/api/ops/events/${vars.event.id}/ack`, { method: 'POST', body: '{}' })
      await qc.invalidateQueries({ queryKey: ['ops', 'events'] })
    },
    onError: (error) => window.alert(errorMessage(error)),
  })
  return (
    <section className="grid min-w-0 gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {q.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
        <Select value={state} onValueChange={setState}>
          <SelectTrigger className="w-36"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="unacked">未确认</SelectItem>
            <SelectItem value="unread">未读</SelectItem>
            <SelectItem value="acked">已确认</SelectItem>
            <SelectItem value="all">全部</SelectItem>
          </SelectContent>
        </Select>
        <Button variant="outline" size="sm" onClick={() => void q.refetch()}><RefreshCcw className="size-4" />刷新</Button>
      </div>
      <FormError error={q.error || markRead.error || ack.error} />
      {q.isLoading && <EmptyPanel text="加载中..." />}
      {!q.isLoading && (q.data?.length ?? 0) === 0 && <EmptyPanel text="暂无事件" />}
      <div className="grid min-w-0 gap-3">
        {(q.data ?? []).map((event) => (
          <Card key={event.id} className={cn('bg-card', !event.read && 'border-primary/40')}>
            <CardContent className="grid gap-3">
              <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <SeverityBadge severity={event.severity} />
                    <span className="font-medium text-foreground">{event.title || eventLabels[event.type] || event.type}</span>
                    <span className="text-xs text-muted-foreground">{fmtTime(event.created_at)}</span>
                  </div>
                  <div className="mt-1 break-words text-sm text-muted-foreground">{event.message}</div>
                </div>
                <div className="flex shrink-0 flex-wrap justify-end gap-1.5">
                  {event.actions.map((item) => (
                    <Button key={item} variant="outline" size="sm" onClick={() => action.mutate({ event, action: item })} disabled={action.isPending}>
                      {action.isPending ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
                      {actionLabel(item)}
                    </Button>
                  ))}
                  {!event.read && <Button variant="outline" size="sm" onClick={() => markRead.mutate(event.id)}><Check className="size-4" />已读</Button>}
                  {!event.acked && <Button size="sm" onClick={() => ack.mutate(event.id)}><CheckCircle2 className="size-4" />确认</Button>}
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  )
}

async function runEventAction(event: OpsEvent, action: string) {
  if (action === 'check_card' && event.target_id) return api(`/api/cards/${event.target_id}/check`, { method: 'POST', body: '{}' })
  if (action === 'check_upstream' && event.target_id) return api(`/api/upstreams/${event.target_id}/check`, { method: 'POST', body: '{}' })
  if (action === 'sync_keys' && event.target_id) return api(`/api/upstreams/${event.target_id}/sync-keys`, { method: 'POST', body: '{}' })
  if (action === 'scheduler_restore' && event.target_id) return api(`/api/cards/${event.target_id}/scheduler/status`, { method: 'POST', body: JSON.stringify({ status: 1 }) })
  if (action === 'refresh_cliproxy_accounts') return api('/api/pools/cliproxy/accounts')
  throw new Error('该动作缺少目标')
}

function AuditTab() {
  const [action, setAction] = useState('')
  const [target, setTarget] = useState('')
  const q = useQuery({ queryKey: ['ops', 'audit', action, target], queryFn: () => api<AuditLog[]>(`/api/ops/audit?action=${encodeURIComponent(action)}&target=${encodeURIComponent(target)}`) })
  return (
    <section className="grid min-w-0 gap-3">
      <div className="grid gap-2 sm:grid-cols-2">
        <Input value={action} placeholder="动作" onChange={(event) => setAction(event.target.value)} />
        <Input value={target} placeholder="目标" onChange={(event) => setTarget(event.target.value)} />
      </div>
      <FormError error={q.error} />
      {q.isLoading && <EmptyPanel text="加载中..." />}
      {!q.isLoading && (q.data?.length ?? 0) === 0 && <EmptyPanel text="暂无审计记录" />}
      {(q.data ?? []).length > 0 && (
        <Card className="bg-card">
          <CardContent>
            <div className="overflow-x-auto rounded-sm border border-border">
              <table className="w-full min-w-[900px] text-left text-sm">
                <thead className="bg-secondary text-xs text-muted-foreground">
                  <tr><th className="px-3 py-2">时间</th><th className="px-3 py-2">用户</th><th className="px-3 py-2">动作</th><th className="px-3 py-2">目标</th><th className="px-3 py-2">字段</th><th className="px-3 py-2">摘要</th></tr>
                </thead>
                <tbody>
                  {(q.data ?? []).map((row) => <AuditRow key={row.id} row={row} />)}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}
    </section>
  )
}

function AuditRow({ row }: { row: AuditLog }) {
  const fields = row.fields ?? []
  return (
    <tr className="border-t border-border align-top">
      <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">{fmtTime(row.created_at)}</td>
      <td className="px-3 py-2">{row.actor || '-'}</td>
      <td className="px-3 py-2">{row.action}</td>
      <td className="px-3 py-2">{[row.target_type, row.target_id].filter(Boolean).join(' / ') || '-'}</td>
      <td className="max-w-72 px-3 py-2 text-muted-foreground">{fields.join(', ') || '-'}</td>
      <td className="px-3 py-2 text-muted-foreground">{row.summary}</td>
    </tr>
  )
}

function NotificationsTab() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['ops', 'notifications'], queryFn: () => api<NotificationRules>('/api/ops/notifications') })
  const [draft, setDraft] = useState<NotificationRules | null>(null)
  const data = draft ?? q.data
  const save = useMutation({
    mutationFn: () => api<NotificationRules>('/api/ops/notifications', { method: 'PATCH', body: JSON.stringify(data) }),
    onSuccess: async (rules) => {
      setDraft(null)
      qc.setQueryData(['ops', 'notifications'], rules)
    },
  })
  const test = useMutation({ mutationFn: () => api('/api/ops/notifications/test', { method: 'POST', body: '{}' }), onError: (error) => window.alert(errorMessage(error)) })
  if (!data) return <ShellLoading />
  const update = (patch: Partial<NotificationRules>) => setDraft({ ...data, ...patch })
  return (
    <section className="grid max-w-2xl gap-3">
      <FormError error={q.error || save.error} />
      <Card className="bg-card">
        <CardHeader><CardTitle className="flex items-center gap-2"><Bell className="size-4" />Telegram 通知</CardTitle></CardHeader>
        <CardContent className="grid gap-4">
          <Field label="全局开关">
            <Select value={data.enabled ? 'true' : 'false'} onValueChange={(value) => update({ enabled: value === 'true' })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value="true">启用</SelectItem><SelectItem value="false">停用</SelectItem></SelectContent>
            </Select>
          </Field>
          <Field label="失败次数阈值">
            <Input type="number" min={1} value={data.failure_threshold} onChange={(event) => update({ failure_threshold: Number(event.target.value) })} />
          </Field>
          <Field label="恢复通知">
            <Select value={data.recovery ? 'true' : 'false'} onValueChange={(value) => update({ recovery: value === 'true' })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value="true">开启</SelectItem><SelectItem value="false">关闭</SelectItem></SelectContent>
            </Select>
          </Field>
          <div className="grid gap-2 sm:grid-cols-2">
            {Object.keys(eventLabels).map((key) => (
              <label key={key} className="flex items-center gap-2 rounded-sm border border-border bg-background px-3 py-2 text-sm">
                <input type="checkbox" checked={data.event_types[key] ?? false} onChange={(event) => update({ event_types: { ...data.event_types, [key]: event.target.checked } })} />
                {eventLabels[key]}
              </label>
            ))}
          </div>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}保存</Button>
            <Button variant="outline" onClick={() => test.mutate()} disabled={test.isPending}>{test.isPending ? <Loader2 className="size-4 animate-spin" /> : <Bell className="size-4" />}测试</Button>
          </div>
        </CardContent>
      </Card>
    </section>
  )
}

function ProfitTab() {
  const [windowValue, setWindowValue] = useState('today')
  const q = useQuery({ queryKey: ['ops', 'profit', windowValue], queryFn: () => api<ProfitResponse>(`/api/ops/profit?window=${windowValue}`) })
  return (
    <section className="grid min-w-0 gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {q.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
        <Select value={windowValue} onValueChange={setWindowValue}><SelectTrigger className="w-28"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="today">today</SelectItem><SelectItem value="24h">24h</SelectItem><SelectItem value="7d">7d</SelectItem></SelectContent></Select>
      </div>
      <FormError error={q.error} />
      <div className="grid gap-3 md:grid-cols-3">
        <Metric label="收入" value={num(q.data?.revenue)} accent="success" />
        <Metric label="成本" value={num(q.data?.cost)} accent={q.data?.cost ? 'danger' : undefined} />
        <Metric label="利润" value={num(q.data?.profit)} accent={(q.data?.profit ?? 0) >= 0 ? 'success' : 'danger'} />
      </div>
      <Card className="bg-card">
        <CardContent className="grid gap-2 text-sm">
          <div className="text-muted-foreground">{q.data?.note}</div>
          {(q.data?.upstream_cost ?? []).map((row) => (
            <div key={row.upstream_id} className="flex items-center justify-between gap-3 border-t border-border pt-2">
              <span className="truncate">{row.name}</span>
              <span>{num(row.cost)}</span>
            </div>
          ))}
          {!q.isLoading && (q.data?.upstream_cost.length ?? 0) === 0 && <div className="text-muted-foreground">暂无成本快照</div>}
        </CardContent>
      </Card>
    </section>
  )
}

function SelfCheckTab() {
  const q = useQuery({ queryKey: ['ops', 'self-check'], queryFn: () => api<SelfCheckResponse>('/api/ops/self-check') })
  return (
    <section className="grid min-w-0 gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {q.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
        <Button variant="outline" size="sm" onClick={() => void q.refetch()}><RefreshCcw className="size-4" />刷新</Button>
      </div>
      <FormError error={q.error} />
      {q.isLoading && <EmptyPanel text="加载中..." />}
      <div className="grid gap-3 md:grid-cols-2">
        {(q.data?.items ?? []).map((item) => (
          <Card key={item.name} className="bg-card">
            <CardContent className="flex items-start gap-3">
              <ShieldCheck className={cn('mt-0.5 size-5', item.status === 'ok' ? 'text-success' : item.status === 'error' ? 'text-destructive' : 'text-warning')} />
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium text-foreground">{item.name}</span>
                  <Badge variant={item.status === 'ok' ? 'success' : item.status === 'error' ? 'destructive' : 'secondary'}>{item.status}</Badge>
                </div>
                {item.message && <div className="mt-1 break-words text-sm text-muted-foreground">{item.message}</div>}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  )
}

function SeverityBadge({ severity }: { severity: string }) {
  if (severity === 'success') return <Badge variant="success">恢复</Badge>
  if (severity === 'warning') return <Badge variant="destructive">告警</Badge>
  return <Badge variant="secondary">{severity || 'info'}</Badge>
}

function actionLabel(action: string) {
  return ({ check_card: '重测卡片', check_upstream: '刷新上游', sync_keys: '同步 Key', scheduler_restore: '恢复调度', refresh_cliproxy_accounts: '刷新号池' } as Record<string, string>)[action] ?? action
}
