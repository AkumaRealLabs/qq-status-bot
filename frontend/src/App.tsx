import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, BarChart3, Database, ExternalLink, KeyRound, Loader2, LogOut, MessageSquare, Settings, SlidersHorizontal, WalletCards } from 'lucide-react'
import { BrandIcon, MobileTabs, NavItem, ShellLoading } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { LoginPage, SetupPage } from '@/features/auth/AuthPages'
import { BalancesPage } from '@/features/balances/BalancesPage'
import { MessagesPage } from '@/features/messages/MessagesPage'
import { CLIProxyPoolPage } from '@/features/pools/CLIProxyPoolPage'
import { RevenuePage } from '@/features/revenue/RevenuePage'
import { SchedulerPage } from '@/features/scheduler/SchedulerPage'
import { SettingsPage } from '@/features/settings/SettingsPage'
import { AdminStatusPage, PublicStatusPage } from '@/features/status/StatusPage'
import { UpstreamsPage } from '@/features/upstreams/UpstreamsPage'
import { api, ApiError } from '@/lib/api'
import { errorMessage } from '@/lib/format'
import type { NavTab, SettingsData, SiteSettings, TabID } from '@/types'

const tabs: NavTab[] = [
  { id: 'status', label: '状态监控', short: '状态', icon: Activity },
  { id: 'balances', label: '余额监控', short: '余额', icon: WalletCards },
  { id: 'revenue', label: '今日收入', short: '收入', icon: BarChart3 },
  { id: 'messages', label: '最新消息', short: '消息', icon: MessageSquare },
  { id: 'upstreams', label: '上游管理', short: '上游', icon: Database },
  { id: 'scheduler', label: '调度器', short: '调度', icon: SlidersHorizontal },
  { id: 'pools', label: '号池管理', short: '号池', icon: KeyRound },
  { id: 'settings', label: '设置', short: '设置', icon: Settings },
]

const tabPaths: Record<TabID, string> = {
  status: '/admin/status',
  balances: '/admin/balances',
  revenue: '/admin/revenue',
  messages: '/admin/messages',
  upstreams: '/admin/upstreams',
  scheduler: '/admin/scheduler',
  pools: '/admin/pools',
  settings: '/admin/settings',
}

function tabFromPath(pathname: string): TabID {
  if (pathname === '/admin/merchant-balance') return 'revenue'
  return tabs.find((item) => tabPaths[item.id] === pathname)?.id ?? 'status'
}

function adminPath(pathname: string) {
  return pathname === '/admin' || pathname.startsWith('/admin/')
}

export default function App() {
  const qc = useQueryClient()
  const [tab, setTab] = useState<TabID>(() => tabFromPath(location.pathname))
  const [loggingOut, setLoggingOut] = useState(false)
  const setup = useQuery({ queryKey: ['setup'], queryFn: () => api<{ initialized: boolean }>('/api/setup/status'), retry: 2 })
  const isAdmin = adminPath(location.pathname)
  const me = useQuery({
    queryKey: ['me'],
    queryFn: () => api('/api/auth/me'),
    retry: (count, error) => !(error instanceof ApiError && error.status === 401) && count < 2,
    enabled: Boolean(setup.data?.initialized && isAdmin),
  })
  const publicSettings = useQuery({ queryKey: ['public-settings'], queryFn: () => api<SiteSettings>('/api/public/settings') })
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api<SettingsData>('/api/settings'), enabled: me.isSuccess && isAdmin })
  const site = settings.data ?? publicSettings.data

  useEffect(() => {
    const onPopState = () => setTab(tabFromPath(location.pathname))
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  useEffect(() => {
    const cfg = site
    if (!cfg) return
    document.title = cfg.site_name || 'AI 上游监控'
    const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]') ?? document.createElement('link')
    icon.rel = 'icon'
    icon.href = cfg.site_icon || '/favicon.ico'
    document.head.appendChild(icon)
  }, [site])

  if (setup.isPending) return <ShellLoading />
  if (setup.isError) return <GateError message="无法加载系统状态" error={setup.error} onRetry={() => void setup.refetch()} />
  if (!setup.data?.initialized) {
    if (!isAdmin) return <PublicStatusPage site={site} />
    return <SetupPage site={site} onDone={() => void setup.refetch()} />
  }
  if (!isAdmin) return <PublicStatusPage site={site} />
  if (me.isPending) return <ShellLoading />
  if (me.isError) {
    if (me.error instanceof ApiError && me.error.status === 401) return <LoginPage site={site} onDone={() => void me.refetch()} />
    return <GateError message="无法确认登录状态" error={me.error} onRetry={() => void me.refetch()} />
  }

  const active = tabs.find((item) => item.id === tab) ?? tabs[0]
  const siteName = site?.site_name || 'AI 上游监控'
  const siteIcon = site?.site_icon || ''
  const navigate = (next: TabID) => {
    setTab(next)
    if (location.pathname !== tabPaths[next]) window.history.pushState(null, '', tabPaths[next])
  }
  const logout = async () => {
    setLoggingOut(true)
    try {
      await api('/api/auth/logout', { method: 'POST' })
      qc.clear()
      location.reload()
    } catch (error) {
      setLoggingOut(false)
      window.alert(errorMessage(error))
    }
  }

  return (
    <div className="min-h-svh bg-background text-body">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-64 border-r border-border bg-sidebar lg:flex lg:flex-col">
        <div className="flex h-16 items-center gap-3 border-b border-border px-4">
          <BrandIcon src={siteIcon} />
          <div className="min-w-0">
            <div className="truncate font-display text-xl font-normal leading-none text-foreground">{siteName}</div>
          </div>
        </div>
        <nav className="grid gap-1 p-3">
          {tabs.map((item) => (
            <NavItem key={item.id} item={item} active={tab === item.id} onClick={() => navigate(item.id)} />
          ))}
        </nav>
        <div className="mt-auto border-t border-border p-3">
          <Button asChild variant="ghost" className="w-full justify-start">
            <a href="/">
              <ExternalLink className="size-4" />
              前台
            </a>
          </Button>
          <Button variant="ghost" className="w-full justify-start" onClick={logout} disabled={loggingOut}>
            {loggingOut ? <Loader2 className="size-4 animate-spin" /> : <LogOut className="size-4" />}
            退出登录
          </Button>
        </div>
      </aside>

      <div className="min-w-0 lg:pl-64">
        <header className="sticky top-0 z-20 flex h-16 items-center justify-between border-b border-border bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80 lg:px-6">
          <div className="min-w-0">
            <div className="text-base font-medium text-foreground">{active.label}</div>
          </div>
          <div className="flex shrink-0 items-center gap-2 lg:hidden">
            <Button asChild variant="outline" size="sm">
              <a href="/">
                <ExternalLink className="size-4" />
                前台
              </a>
            </Button>
            <Button variant="outline" size="sm" onClick={logout} disabled={loggingOut}>
              {loggingOut ? <Loader2 className="size-4 animate-spin" /> : <LogOut className="size-4" />}
              退出
            </Button>
          </div>
        </header>

        <main className="mx-auto grid w-full max-w-[1200px] min-w-0 animate-in gap-4 p-4 fade-in-50 duration-300 lg:p-6">
          <MobileTabs tab={tab} setTab={navigate} tabs={tabs} />
          {tab === 'status' && <AdminStatusPage />}
          {tab === 'balances' && <BalancesPage />}
          {tab === 'revenue' && <RevenuePage />}
          {tab === 'messages' && <MessagesPage />}
          {tab === 'upstreams' && <UpstreamsPage />}
          {tab === 'scheduler' && <SchedulerPage />}
          {tab === 'pools' && <CLIProxyPoolPage />}
          {tab === 'settings' && <SettingsPage />}
        </main>
      </div>
    </div>
  )
}

function GateError({ message, error, onRetry }: { message: string; error: unknown; onRetry: () => void }) {
  return (
    <div className="grid min-h-svh place-items-center bg-background p-4">
      <div className="grid max-w-sm gap-3 rounded-sm border border-border bg-card p-4 text-sm">
        <div className="font-medium text-foreground">{message}</div>
        <div className="break-words text-muted-foreground">{errorMessage(error)}</div>
        <Button variant="outline" size="sm" onClick={onRetry}>
          重试
        </Button>
      </div>
    </div>
  )
}
