import { type ReactNode } from 'react'
import { Pause, RefreshCcw } from 'lucide-react'
import { HoverText, Metric, MiniStat, StatusBadge } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { fmtTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ModelCard, MonitorStatus, Probe, PublicModelCard } from '@/types'

export function StatusCardGroups<T extends ModelCard | PublicModelCard>({
  cards,
  render,
}: {
  cards: T[]
  render: (card: T, index: number) => ReactNode
}) {
  return (
    <div className="grid min-w-0 gap-5">
      {groupCards(cards).map((group) => (
        <section key={group.name} className="grid min-w-0 gap-2">
          <div className="text-sm font-medium text-foreground">{group.name}</div>
          <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
            {group.cards.map(render)}
          </div>
        </section>
      ))}
    </div>
  )
}

export function StatusSummary({ data }: { data?: Pick<MonitorStatus, 'requests' | 'success' | 'failed' | 'success_rate' | 'avg_latency'> }) {
  const rate = `${Math.round(data?.success_rate ?? 0)}%`
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
      <Metric label="请求数" value={data?.requests ?? 0} />
      <Metric label="成功" value={data?.success ?? 0} accent="success" />
      <Metric label="失败" value={data?.failed ?? 0} accent="danger" />
      <Metric label="成功率" value={rate} />
      <Metric label="平均延迟" value={`${data?.avg_latency ?? 0} ms`} />
    </div>
  )
}


export function StatusMonitorCard({
  card,
  windowValue,
  publicView,
  adminActions,
  footerMessage,
}: {
  card: ModelCard | PublicModelCard
  windowValue: string
  publicView?: boolean
  adminActions?: ReactNode
  footerMessage?: string
}) {
  const history = card.history ?? []
  const latest = history.at(-1)
  const muted = card.probe_muted
  const autoProbePaused = isModelCard(card) ? !card.enabled : card.auto_probe_paused
  const historySlots = emptyHistorySlots(windowValue)
  const bucketedHistory = bucketProbeHistory(history, windowValue)
  const populatedSlots = bucketedHistory.filter((probe): probe is Probe => Boolean(probe))
  const samplesInsufficient = populatedSlots.length <= 1
  const ok = muted || (latest ? probeOK(latest) : !card.last_error)
  const statusText = muted ? '静默测试中' : latest ? probeStatusLabel(probeStatus(latest)) : ok ? probeStatusLabel('operational') : probeStatusLabel('failed')
  const successCount = populatedSlots.filter(probeOK).length
  const uptime = populatedSlots.length ? `${((successCount / populatedSlots.length) * 100).toFixed(2)}%` : '-'
  const editableCard = isModelCard(card) ? card : undefined
  const groupName = editableCard?.key_group || (editableCard?.base_url ? '自定义' : '-')
  const displayGroup = cardDisplayGroup(card)
  const ratio = editableCard?.effective_ratio || editableCard?.key_group_ratio || '-'
  const message = footerMessage || card.last_error
  return (
    <Card className={cn('min-w-0 bg-card', muted || autoProbePaused ? 'border-border' : !ok && 'border-destructive/40')}>
      <CardHeader className="min-h-16 gap-2 border-b border-border">
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
          <div className="min-w-0 pt-1">
            <CardTitle className="break-words text-lg leading-tight">{cardTitle(card, ratio)}</CardTitle>
            {!publicView && (
              <CardDescription className="mt-1.5 grid gap-0.5 text-xs leading-relaxed">
                <span>展示分组：{displayGroup}</span>
                <span>Key 分组：{groupName}</span>
                <span>模型：{editableCard?.model || 'gpt-5.6-sol'}</span>
              </CardDescription>
            )}
          </div>
          <div className="flex shrink-0 flex-col items-end gap-1.5">
            {autoProbePaused ? <Badge variant="amber"><Pause className="size-3" />已暂停自动探测</Badge> : muted ? <Badge variant="outline" className="text-muted-foreground"><RefreshCcw className="size-3" />{statusText}</Badge> : <StatusBadge ok={ok} okText={statusText} failText={statusText} />}
            {!publicView && adminActions}
          </div>
        </div>
      </CardHeader>
      <CardContent className="grid gap-2.5 pt-2.5">
        <div className="grid min-w-0 grid-cols-2 gap-2">
          <MiniStat label="对话延迟" value={latest ? `${latest.latency_ms} ms` : '-'} />
          <MiniStat label="调用状态" value={latest ? (latest.http_status ? String(latest.http_status) : probeStatusLabel(probeStatus(latest))) : '-'} />
        </div>
        <div className="min-w-0 border-t border-border pt-2.5">
          <div className="mb-2 flex items-end justify-between gap-2">
            <div className="text-xs text-muted-foreground">可用性 · {windowValue}</div>
            <div className={cn('font-display text-2xl font-normal', muted || autoProbePaused || samplesInsufficient ? 'text-muted-foreground' : ok ? 'text-success' : 'text-destructive')}>{samplesInsufficient ? '样本不足' : uptime}</div>
          </div>
          <div className="-mx-1 overflow-x-auto px-1 pb-1">
            <div
              className="grid min-w-full gap-1"
              style={{ gridTemplateColumns: `repeat(${historySlots}, minmax(6px, 1fr))` }}
            >
              {bucketedHistory.map((probe, index) => {
                if (!probe) return <span key={`missing-${index}`} className="h-4 rounded-xs bg-surface-cream-strong" />
                const good = probeOK(probe)
                return (
                  <HoverText
                    key={`${probe.checked_at}-${index}`}
                    value={probeHoverTitle(probe)}
                    content={<ProbeTooltip probe={probe} muted={muted} />}
                    nativeTitle={false}
                    className={cn('h-4 rounded-xs', muted ? 'bg-muted-soft' : good ? 'bg-success' : 'bg-destructive')}
                  >
                    <span className="sr-only">{probeStatus(probe)}</span>
                  </HoverText>
                )
              })}
            </div>
          </div>
          <div className="mt-2 flex justify-between text-xs text-muted-foreground">
            <span>PAST</span>
            <span>{history.length} 次记录</span>
            <span>NOW</span>
          </div>
        </div>
        {message && (
          <HoverText
            value={message}
            className={cn(
              'rounded-md border px-2.5 py-1.5 text-xs',
              muted || footerMessage === '检查完成' || footerMessage
                ? 'border-border bg-secondary text-muted-foreground'
                : 'border-destructive/30 bg-destructive/10 text-destructive',
            )}
          />
        )}
      </CardContent>
    </Card>
  )
}



export function probeStatus(probe: Probe) {
  return probe.status || (probe.success ? 'operational' : 'failed')
}

export function probeOK(probe: Probe) {
  const status = probeStatus(probe)
  return ['operational', 'degraded', '正常', '延迟偏高'].includes(status)
}

export function emptyHistorySlots(windowValue: string) {
  return ({ '1h': 12, '3h': 18, '5h': 20, '1d': 24, '7d': 28, '15d': 30 } as Record<string, number>)[windowValue] || 24
}

function historyWindowMilliseconds(windowValue: string) {
  return ({ '1h': 60, '3h': 180, '5h': 300, '1d': 1440, '7d': 10080, '15d': 21600 } as Record<string, number>)[windowValue] * 60 * 1000 || 60 * 60 * 1000
}

function probeTime(probe: Probe) {
  const value = Date.parse(probe.checked_at)
  return Number.isFinite(value) ? value : 0
}

// 每个时间桶只展示一个代表探测，避免探测频率差异把某张卡的格子撑得更多。
export function bucketProbeHistory(history: Probe[], windowValue: string, now = Date.now()): Array<Probe | undefined> {
  const slotCount = emptyHistorySlots(windowValue)
  const slots: Array<Probe | undefined> = Array.from({ length: slotCount })
  const start = now - historyWindowMilliseconds(windowValue)
  const slotDuration = historyWindowMilliseconds(windowValue) / slotCount
  for (const probe of history) {
    const checkedAt = probeTime(probe)
    if (checkedAt < start || checkedAt > now) continue
    const index = Math.min(slotCount - 1, Math.floor((checkedAt - start) / slotDuration))
    const existing = slots[index]
    if (!existing || (probeOK(existing) && !probeOK(probe)) || (probeOK(existing) === probeOK(probe) && probeTime(probe) > probeTime(existing))) {
      slots[index] = probe
    }
  }
  return slots
}

export function probeStatusLabel(status: string) {
  return ({
    operational: '正常',
    degraded: '延迟偏高',
    failed: '请求失败',
    error: '探测异常',
    正常: '正常',
    延迟偏高: '延迟偏高',
    验证失败: '验证失败',
    请求失败: '请求失败',
    探测异常: '探测异常',
  } as Record<string, string>)[status] || status || '-'
}

function ProbeTooltip({ probe, muted = false }: { probe: Probe; muted?: boolean }) {
  const ok = probeOK(probe)
  const rows = [
    ['状态', muted ? '静默测试中' : probeStatusLabel(probeStatus(probe)), muted ? 'text-muted-foreground' : ok ? 'text-success' : 'text-destructive'],
    ['延迟', `${probe.latency_ms} ms`],
    ['HTTP 状态', probe.http_status || '-'],
    ['测什么', probePurpose(probe)],
    ['探活输入', probe.input || '-'],
    ['模型回答', probe.output || '-'],
    ['检查时间', fmtTime(probe.checked_at)],
    probe.error ? ['详情', probe.error, muted ? 'text-muted-foreground' : 'text-destructive'] : undefined,
  ].filter(Boolean) as string[][]
  return (
    <span className="block min-w-64 max-w-[min(520px,calc(100vw-32px))] rounded-md bg-popover px-3 py-3 text-sm leading-[1.55] text-popover-foreground">
      <span className="mb-2 block border-b border-hairline-soft pb-2 text-[13px] font-medium leading-[1.4] text-muted-foreground">探测详情</span>
      <span className="grid gap-1.5">
        {rows.map(([label, value, tone]) => (
          <span key={label} className="grid grid-cols-[72px_minmax(0,1fr)] gap-3">
            <span className="whitespace-nowrap text-[13px] font-medium leading-[1.4] text-muted-foreground">{label}</span>
            <span className={cn('whitespace-pre-wrap break-words text-sm leading-[1.55] text-foreground', tone)}>{value}</span>
          </span>
        ))}
      </span>
    </span>
  )
}

export function probeHoverTitle(probe: Probe) {
  return [
    `状态：${probeStatusLabel(probeStatus(probe))}`,
    `延迟：${probe.latency_ms} ms`,
    `HTTP 状态：${probe.http_status || '-'}`,
    `测什么：${probePurpose(probe)}`,
    `探活输入：${probe.input || '-'}`,
    `模型回答：${probe.output || '-'}`,
    `检查时间：${fmtTime(probe.checked_at)}`,
  ].filter(Boolean).join('\n')
}

function probePurpose(_probe: Probe) {
  return '检查 gpt-5.6-sol 响应与连通性'
}

export function isModelCard(card: ModelCard | PublicModelCard): card is ModelCard {
  return 'id' in card
}

export function cardTitle(card: ModelCard | PublicModelCard, ratio: string) {
  if (!isModelCard(card)) return card.name
  if (card.base_url) return card.name
  const detail = ratio !== '-' ? ratio : card.key_name || card.key_group || ''
  return detail ? `${card.upstream_name || card.name} · ${detail}` : card.name
}

export function groupCards<T extends ModelCard | PublicModelCard>(cards: T[]) {
  const groups: { name: string; cards: T[] }[] = []
  for (const card of cards) {
    const name = cardDisplayGroup(card)
    let group = groups.find((item) => item.name === name)
    if (!group) {
      group = { name, cards: [] }
      groups.push(group)
    }
    group.cards.push(card)
  }
  return groups
}

export function cardDisplayGroup(card: ModelCard | PublicModelCard) {
  return card.display_group?.trim() || '其他'
}
