import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { EmptyPanel, SkeletonCardGrid, WindowSelect } from '@/components/common'
import { BrandIcon, Page } from '@/components/layout'
import { api } from '@/lib/api'
import type { PublicMonitorStatus, SiteSettings } from '@/types'
import { StatusCardGroups, StatusMonitorCard, StatusSummary } from './shared'

export function PublicStatusPage({ site }: { site?: SiteSettings }) {
  const [windowValue, setWindowValue] = useState('1h')
  const q = useQuery({
    queryKey: ['public-status', windowValue],
    queryFn: () => api<PublicMonitorStatus>(`/api/public/monitor/status?window=${windowValue}`),
    refetchInterval: 60000,
  })
  const siteName = site?.site_name || 'AI 上游监控'
  const cards = q.data?.rows ?? []
  return (
    <div className="min-h-svh bg-background text-body">
      <header className="border-b border-border bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80">
        <div className="mx-auto flex h-16 w-full max-w-[1200px] items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3" onDoubleClick={() => { location.href = '/admin' }}>
            <BrandIcon src={site?.site_icon} />
            <div className="truncate font-display text-xl font-normal leading-none text-foreground">{siteName}</div>
          </div>
        </div>
      </header>
      <main className="mx-auto grid w-full max-w-[1200px] min-w-0 gap-4 p-4 lg:p-6">
        <Page
          title="状态监控"
          actions={
            <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
              {q.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
              <WindowSelect value={windowValue} setValue={setWindowValue} />
            </div>
          }
        >
          <StatusSummary data={q.data} />
          {q.isLoading && <SkeletonCardGrid count={6} />}
          {!q.isLoading && cards.length > 0 && (
            <StatusCardGroups cards={cards} render={(card, index) => <StatusMonitorCard key={`${card.name}-${index}`} card={card} windowValue={windowValue} publicView />} />
          )}
          {!q.isLoading && cards.length === 0 && <EmptyPanel text="暂无公开卡片" />}
        </Page>
      </main>
    </div>
  )
}
