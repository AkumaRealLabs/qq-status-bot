import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Loader2, RefreshCcw, Settings } from 'lucide-react'
import { EmptyPanel, Metric } from '@/components/common'
import { Page } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { api } from '@/lib/api'
import { errorMessage, fmtTime, num } from '@/lib/format'
import type { MerchantBalanceSummary, SettingsData } from '@/types'

export function MerchantBalancePage({ onOpenSettings }: { onOpenSettings: () => void }) {
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api<SettingsData>('/api/settings') })
  const configured = Boolean(settings.data?.epay_base_url && settings.data?.epay_pid && settings.data?.epay_key)
  const q = useQuery({ queryKey: ['merchant-balance'], queryFn: () => api<MerchantBalanceSummary>('/api/merchant-balance'), enabled: configured })
  const summary = q.data
  return (
    <Page
      title="商户余额"
      description="易支付 v1 商户余额"
      actions={
        configured && (
          <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
            {summary?.checked_at && <span className="min-w-0 text-sm text-muted-foreground">最后刷新：{fmtTime(summary.checked_at)}</span>}
            <Button variant="outline" size="sm" onClick={() => void q.refetch()} disabled={q.isFetching}>
              {q.isFetching ? <Loader2 className="size-3 animate-spin" /> : <RefreshCcw className="size-3" />}
              刷新
            </Button>
          </div>
        )
      }
    >
      {settings.isLoading && <EmptyPanel text="加载中" />}
      {settings.isError && <EmptyPanel text={errorMessage(settings.error)} />}
      {!settings.isLoading && !settings.isError && !configured && (
        <Card className="bg-card">
          <CardContent className="grid justify-items-center gap-3 py-12 text-center">
            <div className="text-sm text-muted-foreground">未配置易支付 v1，请先填写 base_url / pid / key。</div>
            <Button variant="outline" size="sm" onClick={onOpenSettings}>
              <Settings className="size-4" />
              去设置
            </Button>
          </CardContent>
        </Card>
      )}
      {configured && q.isLoading && <EmptyPanel text="查询中" />}
      {configured && q.isError && <EmptyPanel text={errorMessage(q.error)} />}
      {configured && summary && !summary.error && (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <Metric label="商户余额" value={`${num(summary.merchant_balance)} 元`} />
        </div>
      )}
      {configured && summary?.error && (
        <div className="flex min-w-0 items-start gap-2 rounded-sm border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <span className="break-words">{summary.error}</span>
        </div>
      )}
    </Page>
  )
}
