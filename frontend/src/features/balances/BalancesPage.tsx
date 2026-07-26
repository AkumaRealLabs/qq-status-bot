import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCcw, ShieldAlert } from 'lucide-react'
import { EmptyPanel, FeedbackBanner, Metric, SkeletonCardGrid, TypeBadge, StatusBadge } from '@/components/common'
import { Page } from '@/components/layout'
import { BalanceRechargeDialog } from '@/features/upstreams/UpstreamsPage'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { api } from '@/lib/api'
import { fmtTime, latestRefreshTime, num } from '@/lib/format'
import { useFeedback } from '@/lib/feedback'
import { cn } from '@/lib/utils'
import type { BalanceRefreshResult, BalanceRow, BalanceTrendPoint, RunwayEstimate, Upstream } from '@/types'

export function BalancesPage() {
  const qc = useQueryClient()
  const fb = useFeedback('刷新完成')
  const q = useQuery({ queryKey: ['balances'], queryFn: () => api<BalanceRow[]>('/api/monitor/balances'), refetchInterval: 60000 })
  const refresh = useMutation({
    mutationFn: () => api<BalanceRefreshResult>('/api/monitor/balances/refresh', { method: 'POST' }),
    onMutate: () => fb.pending('刷新中...'),
    onSuccess: async (result) => {
      fb.success(result.failed > 0 ? `刷新完成：${result.succeeded} 个成功，${result.failed} 个失败` : '刷新完成')
      await Promise.all([qc.invalidateQueries({ queryKey: ['balances'] }), qc.invalidateQueries({ queryKey: ['upstreams'] })])
    },
    onError: fb.fail,
  })
  const rows = q.data ?? []
  const total = rows.reduce((sum, row) => sum + (row.remain ?? 0), 0)
  const low = rows.filter((row) => row.low_balance).length
  const lastRefresh = latestRefreshTime(rows)
  return (
    <Page
      title="余额监控"
      description="余额、倍率、折算金额与更新时间"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {lastRefresh && <span className="min-w-0 text-sm text-muted-foreground">最后刷新：{fmtTime(lastRefresh)}</span>}
          <Button variant="outline" size="sm" onClick={() => refresh.mutate()} disabled={refresh.isPending}>
            {refresh.isPending ? <Loader2 className="size-3 animate-spin" /> : <RefreshCcw className="size-3" />}
            {refresh.isPending ? '刷新中...' : fb.message === '刷新完成' ? '已刷新' : '刷新余额'}
          </Button>
        </div>
      }
    >
      <FeedbackBanner message={fb.message} error={refresh.isError} success="刷新完成" className="max-w-2xl" />
      <div className="grid gap-3 sm:grid-cols-3">
        <Metric label="上游数量" value={rows.length} />
        <Metric label="折算余额" value={`${num(total)} 元`} />
        <Metric label="低余额" value={low} accent={low > 0 ? 'danger' : 'success'} />
      </div>
      {q.isLoading && <SkeletonCardGrid count={6} />}
      {!q.isLoading && rows.length > 0 && (
        <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((row) => <BalanceMonitorCard key={row.id} row={row} />)}
        </div>
      )}
      {!q.isLoading && rows.length === 0 && <EmptyPanel text="暂无余额数据" />}
    </Page>
  )
}

function runwayText(runway?: RunwayEstimate) {
  if (!runway?.valid) return null
  if (!runway.burning) return '近 24 小时无净消耗'
  const hours = runway.hours_left
  const span = hours >= 48 ? `约 ${num(hours / 24)} 天` : `约 ${num(hours)} 小时`
  return `每小时约消耗 ${num(runway.burn_per_hour)} 元 · 预计可用 ${span}`
}

/** 近 24 小时余额迷你图：单序列无图例；线走弱化色，末点用强调色标记当前值。 */
function BalanceTrend({ trend, runway }: { trend?: BalanceTrendPoint[]; runway?: RunwayEstimate }) {
  const text = runwayText(runway)
  const points = trend ?? []
  if (points.length < 2 && !text) return null
  const W = 240
  const H = 40
  const PAD = 4
  let path = ''
  let area = ''
  let end: { x: number; y: number } | null = null
  if (points.length >= 2) {
    const times = points.map((p) => new Date(p.at).getTime())
    const values = points.map((p) => p.remain)
    const t0 = Math.min(...times)
    const t1 = Math.max(...times)
    const v0 = Math.min(...values)
    const v1 = Math.max(...values)
    const x = (t: number) => (t1 === t0 ? W / 2 : PAD + ((t - t0) / (t1 - t0)) * (W - PAD * 2))
    const y = (v: number) => (v1 === v0 ? H / 2 : H - PAD - ((v - v0) / (v1 - v0)) * (H - PAD * 2))
    const coords = points.map((p, i) => ({ x: x(times[i]), y: y(p.remain) }))
    path = coords.map((c, i) => `${i === 0 ? 'M' : 'L'}${c.x.toFixed(1)},${c.y.toFixed(1)}`).join(' ')
    end = coords[coords.length - 1]
    area = `${path} L${end.x.toFixed(1)},${H} L${coords[0].x.toFixed(1)},${H} Z`
  }
  return (
    <div className="grid gap-1">
      {path && (
        <svg viewBox={`0 0 ${W} ${H}`} className="h-10 w-full" role="img" aria-label="近 24 小时余额趋势" preserveAspectRatio="none">
          <path d={area} fill="var(--accent-teal)" opacity="0.1" />
          <path d={path} fill="none" stroke="var(--muted-soft)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
          {end && (
            <>
              <circle cx={end.x} cy={end.y} r="6" fill="var(--card)" />
              <circle cx={end.x} cy={end.y} r="4" fill="var(--accent-teal)" />
            </>
          )}
        </svg>
      )}
      {text && <div className="text-xs text-muted-foreground">{text}</div>}
    </div>
  )
}

function BalanceMonitorCard({ row }: { row: BalanceRow }) {
  const queryFailed = Boolean(row.error)
  return (
    <Card className={cn('bg-card', (row.low_balance || queryFailed) && 'border-destructive/40')}>
      <CardHeader className="gap-2">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="truncate">{row.name}</CardTitle>
            <CardDescription><TypeBadge type={row.type} /></CardDescription>
          </div>
          <StatusBadge ok={!row.low_balance && !queryFailed} okText="正常" failText={queryFailed ? '查询失败' : '低余额'} />
        </div>
      </CardHeader>
      <CardContent className="grid gap-3">
        <div>
          <div className="text-sm text-muted-foreground">余额折算</div>
          <div className="break-words font-display text-3xl font-normal">{num(row.remain)} 元</div>
          <div className="mt-1.5 text-xs text-muted-foreground">最后刷新：{fmtTime(row.last_check)}</div>
        </div>
        <BalanceTrend trend={row.trend} runway={row.runway} />
        {queryFailed && <div className="whitespace-pre-wrap break-words border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">{row.error}</div>}
        <BalanceRechargeDialog upstream={{ id: row.id, name: row.name, type: row.type as Upstream['type'], base_url: '', enabled: row.enabled, balance_rate: row.balance_rate, low_balance_threshold: 0 }} />
        <Button asChild variant="outline" size="sm">
          <a href={`/admin/costs?upstream_id=${encodeURIComponent(row.id)}`}>
            <ShieldAlert className="size-4" />
            受影响渠道
          </a>
        </Button>
      </CardContent>
    </Card>
  )
}
