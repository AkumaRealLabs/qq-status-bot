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
import type { BalanceRow, Upstream } from '@/types'

export function BalancesPage() {
  const qc = useQueryClient()
  const fb = useFeedback('刷新完成')
  const q = useQuery({ queryKey: ['balances'], queryFn: () => api<BalanceRow[]>('/api/monitor/balances'), refetchInterval: 60000 })
  const refresh = useMutation({
    mutationFn: () => api('/api/monitor/balances/refresh', { method: 'POST' }),
    onMutate: () => fb.pending('刷新中...'),
    onSuccess: async () => {
      fb.success('刷新完成')
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

function BalanceMonitorCard({ row }: { row: BalanceRow }) {
  return (
    <Card className={cn('bg-card', row.low_balance && 'border-destructive/40')}>
      <CardHeader className="gap-2">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="truncate">{row.name}</CardTitle>
            <CardDescription><TypeBadge type={row.type} /></CardDescription>
          </div>
          <StatusBadge ok={!row.low_balance} okText="正常" failText="低余额" />
        </div>
      </CardHeader>
      <CardContent className="grid gap-3">
        <div>
          <div className="text-sm text-muted-foreground">余额折算</div>
          <div className="break-words font-display text-3xl font-normal">{num(row.remain)} 元</div>
          <div className="mt-1.5 text-xs text-muted-foreground">最后刷新：{fmtTime(row.last_check)}</div>
        </div>
        <BalanceRechargeDialog upstream={{ id: row.id, name: row.name, type: row.type as Upstream['type'], base_url: '', enabled: row.enabled, balance_rate: row.balance_rate, low_balance_threshold: 0 }} />
        <Button asChild variant="outline" size="sm">
          <a href={`/admin/scheduler?upstream_id=${encodeURIComponent(row.id)}`}>
            <ShieldAlert className="size-4" />
            受影响渠道
          </a>
        </Button>
      </CardContent>
    </Card>
  )
}
