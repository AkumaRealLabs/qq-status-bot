import { lazy, Suspense, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ShellLoading } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { errorMessage } from '@/lib/format'
import type { SiteSettings } from '@/types'

const PublicStatusPage = lazy(() =>
  import('@/features/status/PublicStatusPage').then((m) => ({ default: m.PublicStatusPage })),
)

export default function PublicApp() {
  const setup = useQuery({
    queryKey: ['setup'],
    queryFn: () => api<{ initialized: boolean }>('/api/setup/status'),
    retry: 2,
  })
  const publicSettings = useQuery({
    queryKey: ['public-settings'],
    queryFn: () => api<SiteSettings>('/api/public/settings'),
  })
  const site = publicSettings.data

  useEffect(() => {
    if (!site) return
    document.title = site.site_name || 'AI 上游监控'
    const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]') ?? document.createElement('link')
    icon.rel = 'icon'
    icon.href = site.site_icon || '/favicon.ico'
    document.head.appendChild(icon)
  }, [site])

  if (setup.isPending) return <ShellLoading />
  if (setup.isError) {
    return <GateError message="无法加载系统状态" error={setup.error} onRetry={() => void setup.refetch()} />
  }

  return (
    <Suspense fallback={<ShellLoading />}>
      <PublicStatusPage site={site} />
    </Suspense>
  )
}

function GateError({ message, error, onRetry }: { message: string; error: unknown; onRetry: () => void }) {
  return (
    <div className="grid min-h-svh place-items-center bg-background p-4">
      <div className="grid max-w-sm gap-3 rounded-lg border border-border bg-card p-5 text-sm">
        <div className="font-medium text-foreground">{message}</div>
        <div className="break-words text-muted-foreground">{errorMessage(error)}</div>
        <Button variant="outline" size="sm" onClick={onRetry}>
          重试
        </Button>
      </div>
    </div>
  )
}
