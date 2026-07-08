import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bell, Check, Loader2, RefreshCcw, ShieldCheck } from 'lucide-react'
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
  return <Page title="事件中心" description="查看最新运维事件"><EventsTab /></Page>
}

export function AuditPage() {
  return <Page title="审计日志" description="查看管理员操作记录"><AuditTab /></Page>
}

export function NotificationsPage() {
  return <Page title="通知规则" description="配置告警事件和 Telegram 测试"><NotificationsTab /></Page>
}

export function ProfitPage() {
  return <Page title="调度池利润" description="按调度器/NewAPI 消费日志计算已确认毛利"><ProfitTab /></Page>
}

export function SelfCheckPage() {
  return <Page title="系统自检" description="轻量检查应用、数据库、浏览器和号池管理连通性"><SelfCheckTab /></Page>
}

function EventsTab() {
  const q = useQuery({ queryKey: ['ops', 'events'], queryFn: () => api<OpsEvent[]>('/api/ops/events'), refetchInterval: 30000 })
  return (
    <section className="grid min-w-0 gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {q.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
        <Button variant="outline" size="sm" onClick={() => void q.refetch()}><RefreshCcw className="size-4" />刷新</Button>
      </div>
      <FormError error={q.error} />
      {q.isLoading && <EmptyPanel text="加载中..." />}
      {!q.isLoading && (q.data?.length ?? 0) === 0 && <EmptyPanel text="暂无事件" />}
      <div className="grid min-w-0 gap-3">
        {(q.data ?? []).map((event) => (
          <Card key={event.id} className="bg-card">
            <CardContent className="grid gap-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <SeverityBadge severity={event.severity} />
                  <span className="font-medium text-foreground">{event.title || eventLabels[event.type] || event.type}</span>
                  <span className="text-xs text-muted-foreground">{fmtTime(event.created_at)}</span>
                </div>
                <div className="mt-1 break-words text-sm text-muted-foreground">{event.message}</div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  )
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
      <div className="grid gap-3 md:grid-cols-4">
        <Metric label="已确认收入" value={num(q.data?.revenue)} accent="success" />
        <Metric label="已确认成本" value={num(q.data?.cost)} accent={q.data?.cost ? 'danger' : undefined} />
        <Metric label="已确认利润" value={num(q.data?.profit)} accent={(q.data?.profit ?? 0) >= 0 ? 'success' : 'danger'} />
        <Metric label="未匹配收入" value={num(q.data?.missing_revenue)} accent={q.data?.missing_revenue ? 'danger' : undefined} />
      </div>
      <div className="text-sm text-muted-foreground">{q.data?.note}</div>
      {q.isLoading && <EmptyPanel text="加载中..." />}
      {!q.isLoading && (q.data?.pools?.length ?? 0) === 0 && <EmptyPanel text="暂无消费日志" />}
      {(q.data?.pools ?? []).map((pool) => (
        <Card key={pool.group} className="bg-card">
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2">
              <span>{pool.tag || pool.group}</span>
              <Badge variant={pool.complete ? 'success' : 'amber'}>{pool.complete ? '完整' : '缺成本绑定'}</Badge>
            </CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3">
            <div className="grid gap-2 text-sm sm:grid-cols-3 lg:grid-cols-6">
              <ProfitMini label="售价" value={num(pool.sale_price)} />
              <ProfitMini label="原始刀数" value={num(pool.usage)} />
              <ProfitMini label="收入" value={num(pool.revenue)} />
              <ProfitMini label="成本" value={num(pool.cost)} />
              <ProfitMini label="利润" value={num(pool.profit)} />
              <ProfitMini label="未匹配收入" value={num(pool.missing_revenue)} />
            </div>
            <div className="overflow-x-auto rounded-sm border border-border">
              <table className="w-full min-w-[980px] text-left text-sm">
                <thead className="bg-secondary text-xs text-muted-foreground">
                  <tr><th className="px-3 py-2">渠道</th><th className="px-3 py-2">绑定卡片</th><th className="px-3 py-2">上游 Key</th><th className="px-3 py-2">成本/刀</th><th className="px-3 py-2">用量</th><th className="px-3 py-2">收入</th><th className="px-3 py-2">成本</th><th className="px-3 py-2">利润</th><th className="px-3 py-2">状态</th></tr>
                </thead>
                <tbody>
                  {pool.channels.map((row) => (
                    <tr key={row.channel_id || row.channel_name} className={cn('border-t border-border align-top', !row.complete && 'bg-destructive/5')}>
                      <td className="px-3 py-2">{row.channel_name || row.channel_id || '-'}</td>
                      <td className="px-3 py-2">{row.card_name || '-'}</td>
                      <td className="px-3 py-2">{[row.upstream_name, row.key_name].filter(Boolean).join(' / ') || '-'}</td>
                      <td className="px-3 py-2">
                        {row.complete ? (
                          <div>
                            <div>{num(row.cost_per_unit)}</div>
                            <div className="text-xs text-muted-foreground">{costSourceLabel(row.cost_source)} · {effectiveLabel(row.cost_effective_from)}</div>
                          </div>
                        ) : '-'}
                      </td>
                      <td className="px-3 py-2">{num(row.usage)}</td>
                      <td className="px-3 py-2">
                        <div>{num(row.revenue)}</div>
                        {row.sale_effective_from && <div className="text-xs text-muted-foreground">售价 {effectiveLabel(row.sale_effective_from)}</div>}
                      </td>
                      <td className="px-3 py-2">{row.complete ? num(row.cost) : '-'}</td>
                      <td className="px-3 py-2">{row.complete ? num(row.profit) : '-'}</td>
                      <td className="px-3 py-2">{row.complete ? <Badge variant="success">已确认</Badge> : <Badge variant="amber">{row.missing_reason || '缺成本绑定'}</Badge>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      ))}
    </section>
  )
}

function ProfitMini({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-sm border border-border bg-background px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-medium">{value}</div>
    </div>
  )
}

function costSourceLabel(value?: string) {
  if (value === 'manual_cost_ratio') return '手动成本'
  if (value === 'upstream_key') return '上游 Key'
  if (value === 'mixed') return '多段来源'
  return value || '-'
}

function effectiveLabel(value?: string) {
  return value === 'mixed' ? '多段' : fmtTime(value)
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
