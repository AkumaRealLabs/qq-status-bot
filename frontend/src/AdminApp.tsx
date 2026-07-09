import { lazy, Suspense, useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  BarChart3,
  Bell,
  Database,
  ExternalLink,
  FileText,
  KeyRound,
  Loader2,
  LogOut,
  MessageSquare,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  TrendingUp,
  WalletCards,
} from 'lucide-react'
import { BrandIcon, MobileTabs, NavItem, ShellLoading } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { LoginPage, SetupPage } from '@/features/auth/AuthPages'
import { api, ApiError } from '@/lib/api'
import { errorMessage } from '@/lib/format'
import type { NavTab, SettingsData, SiteSettings, TabID } from '@/types'

const AdminStatusPage = lazy(() =>
  import('@/features/status/AdminStatusPage').then((m) => ({ default: m.AdminStatusPage })),
)
const BalancesPage = lazy(() => import('@/features/balances/BalancesPage').then((m) => ({ default: m.BalancesPage })))
const MessagesPage = lazy(() => import('@/features/messages/MessagesPage').then((m) => ({ default: m.MessagesPage })))
const AuditPage = lazy(() => import('@/features/ops/OpsPage').then((m) => ({ default: m.AuditPage })))
const EventsPage = lazy(() => import('@/features/ops/OpsPage').then((m) => ({ default: m.EventsPage })))
const NotificationsPage = lazy(() => import('@/features/ops/OpsPage').then((m) => ({ default: m.NotificationsPage })))
const ProfitPage = lazy(() => import('@/features/ops/OpsPage').then((m) => ({ default: m.ProfitPage })))
const SelfCheckPage = lazy(() => import('@/features/ops/OpsPage').then((m) => ({ default: m.SelfCheckPage })))
const CLIProxyPoolPage = lazy(() =>
  import('@/features/pools/CLIProxyPoolPage').then((m) => ({ default: m.CLIProxyPoolPage })),
)
const RevenuePage = lazy(() => import('@/features/revenue/RevenuePage').then((m) => ({ default: m.RevenuePage })))
const SchedulerPage = lazy(() =>
  import('@/features/scheduler/SchedulerPage').then((m) => ({ default: m.SchedulerPage })),
)
const SettingsPage = lazy(() => import('@/features/settings/SettingsPage').then((m) => ({ default: m.SettingsPage })))
const UpstreamsPage = lazy(() =>
  import('@/features/upstreams/UpstreamsPage').then((m) => ({ default: m.UpstreamsPage })),
)

const tabs: NavTab[] = [
  { id: 'status', label: '状态监控', short: '状态', icon: Activity },
  { id: 'balances', label: '余额监控', short: '余额', icon: WalletCards },
  { id: 'revenue', label: '今日收入', short: '收入', icon: BarChart3 },
  { id: 'profit', label: '调度池利润', short: '利润', icon: TrendingUp },
  { id: 'messages', label: '最新消息', short: '消息', icon: MessageSquare },
  { id: 'upstreams', label: '上游管理', short: '上游', icon: Database },
  { id: 'scheduler', label: '调度器', short: '调度', icon: SlidersHorizontal },
  { id: 'pools', label: '号池管理', short: '号池', icon: KeyRound },
  { id: 'events', label: '事件中心', short: '事件', icon: Activity },
  { id: 'audit', label: '审计日志', short: '审计', icon: FileText },
  { id: 'notifications', label: '通知规则', short: '通知', icon: Bell },
  { id: 'self-check', label: '系统自检', short: '自检', icon: ShieldCheck },
  { id: 'settings', label: '设置', short: '设置', icon: Settings },
]

const tabPaths: Record<TabID, string> = {
  status: '/admin/status',
  balances: '/admin/balances',
  revenue: '/admin/revenue',
  profit: '/admin/profit',
  messages: '/admin/messages',
  upstreams: '/admin/upstreams',
  scheduler: '/admin/scheduler',
  pools: '/admin/pools',
  events: '/admin/events',
  audit: '/admin/audit',
  notifications: '/admin/notifications',
  'self-check': '/admin/self-check',
  settings: '/admin/settings',
}

function normalizePath(pathname: string) {
  if (pathname === '/admin/merchant-balance') return '/admin/revenue'
  if (pathname === '/admin/ops' || pathname === '/ops') return '/admin/events'
  return pathname
}

function tabFromPath(pathname: string): TabID {
  const path = normalizePath(pathname)
  return tabs.find((item) => tabPaths[item.id] === path)?.id ?? 'status'
}

export default function AdminApp() {
  const qc = useQueryClient()
  const [tab, setTab] = useState<TabID>(() => tabFromPath(location.pathname))
  const [loggingOut, setLoggingOut] = useState(false)
  const setup = useQuery({
    queryKey: ['setup'],
    queryFn: () => api<{ initialized: boolean }>('/api/setup/status'),
    retry: 2,
  })
  const me = useQuery({
    queryKey: ['me'],
    queryFn: () => api('/api/auth/me'),
    retry: (count, error) => !(error instanceof ApiError && error.status === 401) && count < 2,
    enabled: Boolean(setup.data?.initialized),
  })
  const publicSettings = useQuery({
    queryKey: ['public-settings'],
    queryFn: () => api<SiteSettings>('/api/public/settings'),
  })
  const settings = useQuery({
    queryKey: ['settings'],
    queryFn: () => api<SettingsData>('/api/settings'),
    enabled: me.isSuccess,
  })
  const site = settings.data ?? publicSettings.data

  useEffect(() => {
    const syncLocation = () => {
      const path = normalizePath(location.pathname)
      if (path !== location.pathname) window.history.replaceState(null, '', path)
      setTab(tabFromPath(path))
    }
    syncLocation()
    window.addEventListener('popstate', syncLocation)
    return () => window.removeEventListener('popstate', syncLocation)
  }, [])

  useEffect(() => {
    if (!site) return
    document.title = site.site_name || 'AI 上游监控'
    const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]') ?? document.createElement('link')
    icon.rel = 'icon'
    icon.href = site.site_icon || '/favicon.ico'
    document.head.appendChild(icon)
  }, [site])

  if (setup.isPending) return <ShellLoading />
  if (setup.isError) return <GateError message="无法加载系统状态" error={setup.error} onRetry={() => void setup.refetch()} />
  if (!setup.data?.initialized) {
    return <SetupPage site={site} onDone={() => void setup.refetch()} />
  }
  if (me.isPending) return <ShellLoading />
  if (me.isError) {
    if (me.error instanceof ApiError && me.error.status === 401) {
      return <LoginPage site={site} onDone={() => void me.refetch()} />
    }
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
        <nav className="grid min-h-0 flex-1 content-start gap-1 overflow-y-auto p-3">
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
          <Suspense fallback={<ShellLoading />}>
            {tab === 'status' && <AdminStatusPage />}
            {tab === 'balances' && <BalancesPage />}
            {tab === 'revenue' && <RevenuePage />}
            {tab === 'profit' && <ProfitPage />}
            {tab === 'messages' && <MessagesPage />}
            {tab === 'upstreams' && <UpstreamsPage />}
            {tab === 'scheduler' && <SchedulerPage />}
            {tab === 'pools' && <CLIProxyPoolPage />}
            {tab === 'events' && <EventsPage />}
            {tab === 'audit' && <AuditPage />}
            {tab === 'notifications' && <NotificationsPage />}
            {tab === 'self-check' && <SelfCheckPage />}
            {tab === 'settings' && <SettingsPage />}
          </Suspense>
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
