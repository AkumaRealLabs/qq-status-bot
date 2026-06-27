import { useEffect, useMemo, useRef, useState, type ElementType, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  CheckCircle2,
  ChevronRight,
  Database,
  Download,
  ExternalLink,
  KeyRound,
  Loader2,
  LogOut,
  MonitorCheck,
  Plus,
  RefreshCcw,
  Save,
  Settings,
  Trash2,
  Upload,
  WalletCards,
  XCircle,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'

type Upstream = {
  id: string
  name: string
  type: 'newapi' | 'sub2api'
  base_url: string
  enabled: boolean
  user_id?: string
  access_token?: string
  email?: string
  password?: string
  sub2api_access_token?: string
  sub2api_refresh_token?: string
  balance_rate: number
  low_balance_threshold: number
  last_error?: string
}

type ApiKey = {
  id: string
  upstream_id: string
  name: string
  status: string
  description: string
  group: string
  group_ratio: string
}

type UpstreamRow = {
  upstream: Upstream
  keys: ApiKey[]
  balance?: {
    remain: number
    checked_at: string
    error: string
  }
}

type ModelCard = {
  id: string
  name: string
  upstream_id: string
  upstream_name: string
  key_id: string
  key_name: string
  key_group: string
  key_group_ratio: string
  effective_ratio: string
  enabled: boolean
  last_error: string
  history?: Probe[]
}

type Probe = {
  checked_at: string
  status: string
  input?: string
  output?: string
  success: boolean
  latency_ms: number
  http_status: number
  error: string
}

type BalanceRow = {
  id: string
  name: string
  type: string
  enabled: boolean
  balance_rate: number
  remain?: number
  source_remain?: number
  low_balance?: boolean
  last_check?: string
}

type MonitorStatus = {
  window: string
  requests: number
  success: number
  failed: number
  success_rate: number
  avg_latency: number
  rows: ModelCard[]
}

type SettingsData = {
  check_interval_minutes: number
  telegram_bot_token?: string
  telegram_chat_id?: string
  probe_model: string
  site_name: string
  site_icon: string
}

type SiteSettings = Pick<SettingsData, 'site_name' | 'site_icon'>

type TabID = 'status' | 'balances' | 'upstreams' | 'settings'

const tabs: Array<{ id: TabID; label: string; short: string; icon: ElementType }> = [
  { id: 'status', label: '状态监控', short: '状态', icon: Activity },
  { id: 'balances', label: '余额监控', short: '余额', icon: WalletCards },
  { id: 'upstreams', label: '上游管理', short: '上游', icon: Database },
  { id: 'settings', label: '设置', short: '设置', icon: Settings },
]

const windows = ['1h', '3h', '5h', '1d', '7d', '15d']

const tabPaths: Record<TabID, string> = {
  status: '/status',
  balances: '/balances',
  upstreams: '/upstreams',
  settings: '/settings',
}

const emptyUpstream: Upstream = {
  id: '',
  name: '',
  type: 'newapi',
  base_url: '',
  enabled: true,
  balance_rate: 1,
  low_balance_threshold: 0,
}

let browserLoginWindow: Window | null = null
const browserVNCURL = '/browser/vnc.html?autoconnect=true&resize=scale'

function tabFromPath(pathname: string): TabID {
  return tabs.find((item) => tabPaths[item.id] === pathname)?.id ?? 'status'
}

function closeBrowserLoginWindow() {
  browserLoginWindow?.close()
  browserLoginWindow = null
  const win = window.open('', 'ai-upstream-monitor-vnc')
  win?.close()
}

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: 'include',
    headers: init?.body ? { 'Content-Type': 'application/json', ...init.headers } : init?.headers,
    ...init,
  })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error(data.error || `HTTP ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

export default function App() {
  const qc = useQueryClient()
  const [tab, setTab] = useState<TabID>(() => tabFromPath(location.pathname))
  const [loggingOut, setLoggingOut] = useState(false)
  const setup = useQuery({ queryKey: ['setup'], queryFn: () => api<{ initialized: boolean }>('/api/setup/status') })
  const me = useQuery({ queryKey: ['me'], queryFn: () => api('/api/auth/me'), retry: false, enabled: setup.data?.initialized })
  const publicSettings = useQuery({ queryKey: ['public-settings'], queryFn: () => api<SiteSettings>('/api/public/settings') })
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api<SettingsData>('/api/settings'), enabled: me.isSuccess })
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

  if (setup.isLoading) return <ShellLoading />
  if (!setup.data?.initialized) return <SetupPage site={site} onDone={() => void setup.refetch()} />
  if (me.isError) return <LoginPage site={site} onDone={() => void me.refetch()} />

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
          <Button variant="ghost" className="w-full justify-start" onClick={logout} disabled={loggingOut}>
            {loggingOut ? <Loader2 className="size-4 animate-spin" /> : <LogOut className="size-4" />}
            退出登录
          </Button>
        </div>
      </aside>

      <div className="lg:pl-64">
        <header className="sticky top-0 z-20 flex h-16 items-center justify-between border-b border-border bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80 lg:px-6">
          <div className="min-w-0">
            <div className="text-base font-medium text-foreground">{active.label}</div>
          </div>
          <Button variant="outline" size="sm" className="lg:hidden" onClick={logout} disabled={loggingOut}>
            {loggingOut ? <Loader2 className="size-4 animate-spin" /> : <LogOut className="size-4" />}
            退出
          </Button>
        </header>

        <main className="mx-auto grid w-full max-w-[1200px] animate-in gap-4 p-4 fade-in-50 duration-300 lg:p-6">
          <MobileTabs tab={tab} setTab={navigate} />
          {tab === 'status' && <StatusPage />}
          {tab === 'balances' && <BalancesPage />}
          {tab === 'upstreams' && <UpstreamsPage />}
          {tab === 'settings' && <SettingsPage />}
        </main>
      </div>
    </div>
  )
}

function NavItem({ item, active, onClick }: { item: (typeof tabs)[number]; active: boolean; onClick: () => void }) {
  return (
    <button
      className={cn(
        'flex h-9 items-center gap-2 rounded-sm px-3 text-sm font-medium transition-colors',
        active ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'text-muted-foreground hover:bg-sidebar-accent hover:text-foreground',
      )}
      onClick={onClick}
    >
      <item.icon className="size-4" />
      <span className="flex-1 text-left">{item.label}</span>
      {active && <ChevronRight className="size-4 text-primary" />}
    </button>
  )
}

function BrandIcon({ src, className }: { src?: string; className?: string }) {
  const [failed, setFailed] = useState(false)
  if (src && !failed) {
    return <img src={src} alt="" className={cn('size-9 rounded-lg object-cover', className)} onError={() => setFailed(true)} />
  }
  return (
    <div className={cn('flex size-9 items-center justify-center rounded-lg bg-surface-dark text-on-dark', className)}>
      <MonitorCheck className="size-5" />
    </div>
  )
}

function MobileTabs({ tab, setTab }: { tab: TabID; setTab: (tab: TabID) => void }) {
  return (
    <div className="mb-4 grid grid-cols-4 gap-2 md:hidden">
      {tabs.map((item) => (
        <Button key={item.id} variant={tab === item.id ? 'secondary' : 'outline'} size="sm" onClick={() => setTab(item.id)}>
          <item.icon className="size-4" />
          {item.short}
        </Button>
      ))}
    </div>
  )
}

function ShellLoading() {
  return (
    <div className="grid min-h-svh place-items-center bg-background">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        加载中
      </div>
    </div>
  )
}

function SetupPage({ site, onDone }: { site?: SiteSettings; onDone: () => void }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const mutation = useMutation({
    mutationFn: () => api('/api/setup', { method: 'POST', body: JSON.stringify({ username, password }) }),
    onSuccess: onDone,
  })
  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (!mutation.isPending) mutation.mutate()
  }
  return (
    <AuthFrame site={site} title="初始化管理员" subtitle="创建第一个管理员账号">
      <form className="grid gap-4" onSubmit={submit}>
        <FormError error={mutation.error} />
        <Field label="用户名">
          <Input value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="密码">
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending && <Loader2 className="size-4 animate-spin" />}
          创建管理员
        </Button>
      </form>
    </AuthFrame>
  )
}

function LoginPage({ site, onDone }: { site?: SiteSettings; onDone: () => void }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const mutation = useMutation({
    mutationFn: () => api('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
    onSuccess: onDone,
  })
  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (!mutation.isPending) mutation.mutate()
  }
  return (
    <AuthFrame site={site} title="登录" subtitle={`进入 ${site?.site_name || 'AI 上游监控'} 后台`}>
      <form className="grid gap-4" onSubmit={submit}>
        <FormError error={mutation.error} />
        <Field label="用户名">
          <Input value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="密码">
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending && <Loader2 className="size-4 animate-spin" />}
          登录
        </Button>
      </form>
    </AuthFrame>
  )
}

function AuthFrame({ site, title, subtitle, children }: { site?: SiteSettings; title: string; subtitle: string; children: ReactNode }) {
  return (
    <div className="grid min-h-svh place-items-center bg-background p-4">
      <Card className="animate-in w-full max-w-sm bg-card fade-in-50 zoom-in-95 duration-300">
        <CardHeader className="gap-2 text-center">
          <BrandIcon src={site?.site_icon} className="mx-auto" />
          <CardTitle className="font-display text-3xl font-normal">{title}</CardTitle>
          <CardDescription>{subtitle}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">{children}</CardContent>
      </Card>
    </div>
  )
}

function FormError({ error }: { error: unknown }) {
  if (!error) return null
  return (
    <div className="animate-in rounded-sm border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive fade-in slide-in-from-top-1">
      {(error as Error).message}
    </div>
  )
}

function StatusPage() {
  const [windowValue, setWindowValue] = useState('1h')
  const q = useQuery({
    queryKey: ['status', windowValue],
    queryFn: () => api<MonitorStatus>(`/api/monitor/status?window=${windowValue}`),
    refetchInterval: 60000,
  })
  const cards = q.data?.rows ?? []
  const rate = `${Math.round(q.data?.success_rate ?? 0)}%`
  return (
    <Page
      title="状态监控"
      description="探测结果与最近检查历史"
      actions={
        <div className="flex items-center gap-2">
          {q.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <WindowSelect value={windowValue} setValue={setWindowValue} />
        </div>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="请求数" value={q.data?.requests ?? 0} />
        <Metric label="成功" value={q.data?.success ?? 0} accent="success" />
        <Metric label="失败" value={q.data?.failed ?? 0} accent="danger" />
        <Metric label="成功率" value={rate} />
        <Metric label="平均延迟" value={`${q.data?.avg_latency ?? 0} ms`} />
      </div>
      {q.isLoading && <SkeletonCardGrid count={6} />}
      {!q.isLoading && cards.length > 0 && (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {cards.map((card) => <StatusMonitorCard key={card.id} card={card} windowValue={windowValue} />)}
        </div>
      )}
      {!q.isLoading && cards.length === 0 && <EmptyPanel text="暂无卡片" />}
    </Page>
  )
}

function StatusMonitorCard({ card, windowValue }: { card: ModelCard; windowValue: string }) {
  const qc = useQueryClient()
  const [message, setMessage] = useState('')
  useEffect(() => {
    if (message !== '检查完成') return
    const timer = window.setTimeout(() => setMessage(''), 1800)
    return () => window.clearTimeout(timer)
  }, [message])
  const check = useMutation({
    mutationFn: () => api(`/api/cards/${card.id}/check`, { method: 'POST' }),
    onMutate: () => setMessage('检查中...'),
    onSuccess: async () => {
      setMessage('检查完成')
      await qc.invalidateQueries({ queryKey: ['status'] })
    },
    onError: (error) => setMessage(errorMessage(error)),
  })
  const history = (card.history ?? []).slice(-12)
  const latest = history.at(-1)
  const ok = latest ? probeOK(latest) : !card.last_error
  const statusText = latest ? probeStatusLabel(probeStatus(latest)) : ok ? probeStatusLabel('operational') : probeStatusLabel('failed')
  const successCount = history.filter(probeOK).length
  const uptime = history.length ? `${((successCount / history.length) * 100).toFixed(2)}%` : '-'
  const groupName = card.key_group || '-'
  const ratio = card.effective_ratio || card.key_group_ratio || '-'
  return (
    <Card className={cn('bg-card', !ok && 'border-destructive/40')}>
      <CardHeader className="min-h-16 gap-2 border-b border-border">
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
          <div className="min-w-0 pt-1">
            <CardTitle className="break-words text-lg leading-tight">{card.upstream_name} · {ratio}</CardTitle>
            <CardDescription className="mt-1.5 grid gap-0.5 text-xs leading-relaxed">
              <span>分组：{groupName}</span>
              <span>模型：gpt-5.5</span>
            </CardDescription>
          </div>
          <div className="flex min-w-16 shrink-0 flex-col items-end gap-1.5">
            <StatusBadge ok={ok} okText={statusText} failText={statusText} />
            <Button variant="outline" size="sm" onClick={() => check.mutate()} disabled={check.isPending}>
              {check.isPending ? <Loader2 className="size-3 animate-spin" /> : <RefreshCcw className="size-3" />}
              检查
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="grid gap-2.5 pt-2.5">
        <div className="grid grid-cols-2 gap-2">
          <MiniStat label="对话延迟" value={latest ? `${latest.latency_ms} ms` : '-'} />
          <MiniStat label="端点 PING" value={latest?.http_status ? String(latest.http_status) : '-'} />
        </div>
        <div className="border-t border-border pt-2.5">
          <div className="mb-2 flex items-end justify-between gap-2">
            <div className="text-xs text-muted-foreground">可用性 · {windowValue}</div>
            <div className={cn('font-display text-2xl font-normal', ok ? 'text-success' : 'text-destructive')}>{uptime}</div>
          </div>
          <div className="grid grid-cols-12 gap-1">
            {history.map((probe, index) => {
              const good = probeOK(probe)
              return (
                <HoverText
                  key={`${probe.checked_at}-${index}`}
                  value={probeHover(probe)}
                  className={cn('h-4 rounded-sm', good ? 'bg-success' : 'bg-destructive')}
                >
                  <span className="sr-only">{probeStatus(probe)}</span>
                </HoverText>
              )
            })}
            {history.length === 0 &&
              Array.from({ length: 12 }).map((_, index) => <span key={index} className="h-4 rounded-sm bg-surface-cream-strong" />)}
          </div>
          <div className="mt-2 flex justify-between text-xs text-muted-foreground">
            <span>PAST</span>
            <span>{history.length} 次记录</span>
            <span>NOW</span>
          </div>
        </div>
        {(message || card.last_error) && (
          <HoverText
            value={message || card.last_error}
            className={cn('rounded-sm px-2.5 py-1.5 text-xs', check.isError || card.last_error ? 'bg-destructive/10 text-destructive' : 'bg-secondary text-muted-foreground')}
          />
        )}
      </CardContent>
    </Card>
  )
}

function BalancesPage() {
  const qc = useQueryClient()
  const [message, setMessage] = useState('')
  const q = useQuery({ queryKey: ['balances'], queryFn: () => api<BalanceRow[]>('/api/monitor/balances'), refetchInterval: 60000 })
  useAutoClear(message, '刷新完成', setMessage)
  const refresh = useMutation({
    mutationFn: () => api('/api/monitor/balances/refresh', { method: 'POST' }),
    onMutate: () => setMessage('刷新中...'),
    onSuccess: async () => {
      setMessage('刷新完成')
      await Promise.all([qc.invalidateQueries({ queryKey: ['balances'] }), qc.invalidateQueries({ queryKey: ['upstreams'] })])
    },
    onError: (error) => setMessage(errorMessage(error)),
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
        <div className="flex items-center gap-2">
          {lastRefresh && <span className="text-sm text-muted-foreground">最后刷新：{fmtTime(lastRefresh)}</span>}
          {message && <span className={cn('text-sm', refresh.isError ? 'text-destructive' : 'text-muted-foreground')}>{message}</span>}
          <Button variant="outline" size="sm" onClick={() => refresh.mutate()} disabled={refresh.isPending}>
            {refresh.isPending ? <Loader2 className="size-3 animate-spin" /> : <RefreshCcw className="size-3" />}
            刷新余额
          </Button>
        </div>
      }
    >
      <div className="grid gap-3 sm:grid-cols-3">
        <Metric label="上游数量" value={rows.length} />
        <Metric label="折算余额" value={`${num(total)} 元`} />
        <Metric label="低余额" value={low} accent={low > 0 ? 'danger' : 'success'} />
      </div>
      {q.isLoading && <SkeletonCardGrid count={6} />}
      {!q.isLoading && rows.length > 0 && (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
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
        <div className="flex items-start justify-between gap-3">
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
          <div className="font-display text-3xl font-normal">{num(row.remain)} 元</div>
          <div className="mt-1.5 text-xs text-muted-foreground">最后刷新：{fmtTime(row.last_check)}</div>
        </div>
      </CardContent>
    </Card>
  )
}

function UpstreamsPage() {
  const qc = useQueryClient()
  const [cardError, setCardError] = useState('')
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamRow[]>('/api/upstreams') })
  const cards = useQuery({ queryKey: ['cards'], queryFn: () => api<ModelCard[]>('/api/cards') })
  const createCard = useMutation({
    mutationFn: (body: { upstream_id: string; key_id: string }) => api('/api/cards', { method: 'POST', body: JSON.stringify(body) }),
    onMutate: () => setCardError(''),
    onSuccess: () => void invalidateMonitor(qc),
    onError: (error) => setCardError(errorMessage(error)),
  })
  const upstreamRows = upstreams.data ?? []
  const cardRows = cards.data ?? []
  return (
    <Page
      title="上游管理"
      description="上游凭据、Key 同步和状态卡片"
      actions={
        <div className="flex items-center gap-2">
          {(upstreams.isFetching || cards.isFetching) && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <UpstreamDialog />
        </div>
      }
    >
      <Card>
        <CardHeader>
          <CardTitle>上游</CardTitle>
          <CardDescription>new-api 与 sub2api 普通用户接入</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-3 md:hidden">
            {upstreams.isLoading && <div className="rounded-sm border border-border bg-background p-3 text-sm text-muted-foreground">加载中...</div>}
            {!upstreams.isLoading && upstreamRows.map((row) => <UpstreamMobileItem key={row.upstream.id} row={row} />)}
            {!upstreams.isLoading && upstreamRows.length === 0 && <div className="rounded-sm border border-border bg-background p-3 text-sm text-muted-foreground">暂无上游</div>}
          </div>
          <div className="hidden overflow-x-auto md:block">
            <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>地址</TableHead>
                <TableHead>Key</TableHead>
                <TableHead>余额</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>错误</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {upstreams.isLoading && <SkeletonRows cells={8} />}
              {!upstreams.isLoading &&
                upstreamRows.map((row) => (
                  <UpstreamTableRow key={row.upstream.id} row={row} />
                ))}
              {!upstreams.isLoading && upstreamRows.length === 0 && <EmptyRow colSpan={8} text="暂无上游" />}
            </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="items-start gap-3 sm:flex sm:flex-row sm:items-center sm:justify-between">
          <div>
            <CardTitle>状态卡片</CardTitle>
            <CardDescription>名称由后端按上游名称和 Key 名称生成</CardDescription>
          </div>
          <CardDialog rows={upstreamRows} onSubmit={(body) => createCard.mutate(body)} pending={createCard.isPending} />
        </CardHeader>
        <CardContent>
          {cardError && <FormError error={cardError} />}
          <div className="grid gap-3 md:hidden">
            {cards.isLoading && <div className="rounded-sm border border-border bg-background p-3 text-sm text-muted-foreground">加载中...</div>}
            {!cards.isLoading && cardRows.map((card) => <ManagedCardMobileItem key={card.id} card={card} />)}
            {!cards.isLoading && cardRows.length === 0 && <div className="rounded-sm border border-border bg-background p-3 text-sm text-muted-foreground">暂无卡片</div>}
          </div>
          <div className="hidden overflow-x-auto md:block">
            <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>上游</TableHead>
                <TableHead>Key</TableHead>
                <TableHead>启用</TableHead>
                <TableHead>错误</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {cards.isLoading && <SkeletonRows cells={6} />}
              {!cards.isLoading && cardRows.map((card) => <ManagedCardRow key={card.id} card={card} />)}
              {!cards.isLoading && cardRows.length === 0 && <EmptyRow colSpan={6} text="暂无卡片" />}
            </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
    </Page>
  )
}

function UpstreamTableRow({ row }: { row: UpstreamRow }) {
  const upstream = row.upstream
  const error = upstream.last_error || row.balance?.error || '-'
  return (
    <TableRow>
      <TableCell className="font-medium">{upstream.name}</TableCell>
      <TableCell>
        <TypeBadge type={upstream.type} />
      </TableCell>
      <TableCell className="max-w-72 truncate">{upstream.base_url}</TableCell>
      <TableCell>{keysOf(row).length}</TableCell>
      <TableCell>{num(row.balance?.remain)}</TableCell>
      <TableCell>
        <StatusBadge ok={upstream.enabled} okText="启用" failText="停用" />
      </TableCell>
      <TableCell className="max-w-64 text-destructive">
        <HoverText value={error} />
      </TableCell>
      <TableCell>
        <UpstreamActions row={row} />
      </TableCell>
    </TableRow>
  )
}

function UpstreamMobileItem({ row }: { row: UpstreamRow }) {
  const upstream = row.upstream
  const error = upstream.last_error || row.balance?.error || ''
  return (
    <div className="grid gap-3 rounded-sm border border-border bg-background p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate font-medium">{upstream.name}</div>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <TypeBadge type={upstream.type} />
            <span>Key：{keysOf(row).length}</span>
            <span>余额：{num(row.balance?.remain)}</span>
          </div>
        </div>
        <StatusBadge ok={upstream.enabled} okText="启用" failText="停用" />
      </div>
      <HoverText value={upstream.base_url} className="text-xs text-muted-foreground" />
      {error && <HoverText value={error} className="rounded-sm bg-destructive/10 px-2 py-1 text-xs text-destructive" />}
      <UpstreamActions row={row} mobile />
    </div>
  )
}

function UpstreamActions({ row, mobile }: { row: UpstreamRow; mobile?: boolean }) {
  const qc = useQueryClient()
  const upstream = row.upstream
  const remove = useMutation({
    mutationFn: () => api(`/api/upstreams/${upstream.id}`, { method: 'DELETE' }),
    onSuccess: () => void invalidateMonitor(qc),
    onError: (error) => window.alert(errorMessage(error)),
  })
  return (
    <div className={cn('flex gap-2', mobile ? 'flex-wrap' : 'justify-end')}>
      <UpstreamDialog upstream={upstream} />
      <Action path={`/api/upstreams/${upstream.id}/sync-keys`} label="同步 Key" />
      <Action path={`/api/upstreams/${upstream.id}/check`} label="检查" />
      <IconAction title="删除" onClick={() => confirmDelete(upstream.name) && remove.mutate()} pending={remove.isPending} icon={Trash2} danger />
    </div>
  )
}

function ManagedCardRow({ card }: { card: ModelCard }) {
  const qc = useQueryClient()
  const toggle = useMutation({
    mutationFn: () =>
      api(`/api/cards/${card.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ upstream_id: card.upstream_id, key_id: card.key_id, enabled: !card.enabled }),
      }),
    onSuccess: () => void invalidateMonitor(qc),
    onError: (error) => window.alert(errorMessage(error)),
  })
  const remove = useMutation({
    mutationFn: () => api(`/api/cards/${card.id}`, { method: 'DELETE' }),
    onSuccess: () => void invalidateMonitor(qc),
    onError: (error) => window.alert(errorMessage(error)),
  })
  const error = card.last_error || '-'
  return (
    <TableRow>
      <TableCell className="font-medium">{card.name}</TableCell>
      <TableCell>{card.upstream_name}</TableCell>
      <TableCell>{card.key_name || card.key_group || '-'}</TableCell>
      <TableCell>
        <StatusBadge ok={card.enabled} okText="启用" failText="停用" />
      </TableCell>
      <TableCell className="max-w-72 text-destructive">
        <HoverText value={error} />
      </TableCell>
      <TableCell>
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={() => toggle.mutate()} disabled={toggle.isPending}>
            {toggle.isPending && <Loader2 className="size-3 animate-spin" />}
            {card.enabled ? '停用' : '启用'}
          </Button>
          <IconAction title="删除" onClick={() => confirmDelete(card.name) && remove.mutate()} pending={remove.isPending} icon={Trash2} danger />
        </div>
      </TableCell>
    </TableRow>
  )
}

function ManagedCardMobileItem({ card }: { card: ModelCard }) {
  const qc = useQueryClient()
  const toggle = useMutation({
    mutationFn: () =>
      api(`/api/cards/${card.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ upstream_id: card.upstream_id, key_id: card.key_id, enabled: !card.enabled }),
      }),
    onSuccess: () => void invalidateMonitor(qc),
    onError: (error) => window.alert(errorMessage(error)),
  })
  const remove = useMutation({
    mutationFn: () => api(`/api/cards/${card.id}`, { method: 'DELETE' }),
    onSuccess: () => void invalidateMonitor(qc),
    onError: (error) => window.alert(errorMessage(error)),
  })
  return (
    <div className="grid gap-3 rounded-sm border border-border bg-background p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate font-medium">{card.name}</div>
          <div className="mt-1 truncate text-xs text-muted-foreground">{card.upstream_name} · {card.key_name || card.key_group || '-'}</div>
        </div>
        <StatusBadge ok={card.enabled} okText="启用" failText="停用" />
      </div>
      {card.last_error && <HoverText value={card.last_error} className="rounded-sm bg-destructive/10 px-2 py-1 text-xs text-destructive" />}
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => toggle.mutate()} disabled={toggle.isPending}>
          {toggle.isPending && <Loader2 className="size-3 animate-spin" />}
          {card.enabled ? '停用' : '启用'}
        </Button>
        <IconAction title="删除" onClick={() => confirmDelete(card.name) && remove.mutate()} pending={remove.isPending} icon={Trash2} danger />
      </div>
    </div>
  )
}

function UpstreamDialog({ upstream }: { upstream?: Upstream }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<Upstream>(upstream ?? emptyUpstream)
  const [tokenMessage, setTokenMessage] = useState('')
  useAutoClear(tokenMessage, '浏览器已打开|采集完成', setTokenMessage)
  const save = useMutation({
    mutationFn: () =>
      api(upstream ? `/api/upstreams/${upstream.id}` : '/api/upstreams', {
        method: upstream ? 'PATCH' : 'POST',
        body: JSON.stringify(form),
      }),
    onSuccess: () => {
      setOpen(false)
      void invalidateMonitor(qc)
    },
  })
  const browserLogin = useMutation({
    mutationFn: () => api<{ vnc_url: string }>(`/api/upstreams/${upstream?.id ?? ''}/browser-login`, { method: 'POST' }),
  })
  const browserCapture = useMutation({
    mutationFn: () => api<{ access_token: boolean; refresh_token: boolean }>(`/api/upstreams/${upstream?.id ?? ''}/browser-capture`, { method: 'POST' }),
    onMutate: () => setTokenMessage('采集中...'),
    onSuccess: async (out) => {
      setTokenMessage('采集完成')
      await qc.invalidateQueries({ queryKey: ['upstreams'] })
      if (out.access_token || out.refresh_token) {
        closeBrowserLoginWindow()
      }
    },
    onError: (error) => setTokenMessage(errorMessage(error)),
  })
  const update = (patch: Partial<Upstream>) => setForm((value) => ({ ...value, ...patch }))
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) setForm(upstream ?? emptyUpstream)
      }}
    >
      <DialogTrigger asChild>
        <Button variant={upstream ? 'outline' : 'default'} size="sm">
          {upstream ? '编辑' : <><Plus className="size-4" />新增上游</>}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>{upstream ? '编辑上游' : '新增上游'}</DialogTitle>
        <FormError error={save.error} />
        <div className="grid gap-4 md:grid-cols-2">
          <Field label="名称">
            <Input value={form.name} onChange={(e) => update({ name: e.target.value })} />
          </Field>
          <Field label="类型">
            <Select value={form.type} onValueChange={(value) => update({ type: value as Upstream['type'] })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="newapi">new-api</SelectItem>
                <SelectItem value="sub2api">sub2api</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="Base URL">
            <Input value={form.base_url} onChange={(e) => update({ base_url: e.target.value })} />
          </Field>
          <Field label="状态">
            <Select value={form.enabled ? 'true' : 'false'} onValueChange={(value) => update({ enabled: value === 'true' })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="true">启用</SelectItem>
                <SelectItem value="false">停用</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="余额倍率">
            <Input type="number" value={form.balance_rate} onChange={(e) => update({ balance_rate: Number(e.target.value) })} />
          </Field>
          <Field label="低余额阈值">
            <Input type="number" value={form.low_balance_threshold} onChange={(e) => update({ low_balance_threshold: Number(e.target.value) })} />
          </Field>
          {form.type === 'newapi' && (
            <>
              <Field label="New-Api-User">
                <Input value={form.user_id ?? ''} onChange={(e) => update({ user_id: e.target.value })} />
              </Field>
              <Field label="Access Token">
                <Input value={form.access_token ?? ''} onChange={(e) => update({ access_token: e.target.value })} />
              </Field>
            </>
          )}
        </div>
        {form.type === 'sub2api' && upstream && (
          <div className="rounded-sm border border-border bg-secondary/50 p-3">
            <div className="flex flex-wrap items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setTokenMessage('打开浏览器...')
                  openBrowserLogin(browserLogin, () => setTokenMessage('浏览器已打开'), (error) => setTokenMessage(errorMessage(error)))
                }}
                disabled={browserLogin.isPending}
              >
                {browserLogin.isPending ? <Loader2 className="size-4 animate-spin" /> : <ExternalLink className="size-4" />}
                浏览器登录
              </Button>
              <Button variant="outline" size="sm" onClick={() => browserCapture.mutate()} disabled={browserCapture.isPending}>
                {browserCapture.isPending ? <Loader2 className="size-4 animate-spin" /> : <KeyRound className="size-4" />}
                采集 Token
              </Button>
              {tokenMessage && (
                <span className={cn('text-sm', browserCapture.isError || browserLogin.isError ? 'text-destructive' : 'text-muted-foreground')}>{tokenMessage}</span>
              )}
            </div>
          </div>
        )}
        <div className="flex justify-end">
          <Button onClick={() => save.mutate()} disabled={save.isPending}>
            {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            保存
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function CardDialog({
  rows,
  onSubmit,
  pending,
}: {
  rows: UpstreamRow[]
  onSubmit: (body: { upstream_id: string; key_id: string }) => void
  pending: boolean
}) {
  const [open, setOpen] = useState(false)
  const [upstreamID, setUpstreamID] = useState('')
  const [keyID, setKeyID] = useState('')
  const keys = useMemo(() => keysOf(rows.find((row) => row.upstream.id === upstreamID)), [rows, upstreamID])
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <KeyRound className="size-4" />
          新增卡片
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>新增状态卡片</DialogTitle>
        <Field label="上游">
          <Select
            value={upstreamID}
            onValueChange={(value) => {
              setUpstreamID(value)
              setKeyID('')
            }}
          >
            <SelectTrigger>
              <SelectValue placeholder="选择上游" />
            </SelectTrigger>
            <SelectContent>
              {rows.map((row) => (
                <SelectItem key={row.upstream.id} value={row.upstream.id}>
                  {row.upstream.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="Key">
          <Select value={keyID} onValueChange={setKeyID}>
            <SelectTrigger>
              <SelectValue placeholder="选择 Key" />
            </SelectTrigger>
            <SelectContent>
              {keys.map((key) => (
                <SelectItem key={key.id} value={key.id}>
                  {key.name || key.description || key.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <div className="flex justify-end">
          <Button
            onClick={() => {
              onSubmit({ upstream_id: upstreamID, key_id: keyID })
              setOpen(false)
            }}
            disabled={pending || !upstreamID || !keyID}
          >
            {pending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            保存
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function SettingsPage() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['settings'], queryFn: () => api<SettingsData>('/api/settings') })
  const [form, setForm] = useState<SettingsData | null>(null)
  const [message, setMessage] = useState('')
  const [backupMessage, setBackupMessage] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)
  const data = form ?? q.data
  useAutoClear(message, '已保存', setMessage)
  useAutoClear(backupMessage, '已导出|导入完成', setBackupMessage)
  const save = useMutation({
    mutationFn: () => api('/api/settings', { method: 'PATCH', body: JSON.stringify(data) }),
    onMutate: () => setMessage('保存中...'),
    onSuccess: () => {
      setMessage('已保存')
      setForm(null)
      void qc.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (error) => setMessage(errorMessage(error)),
  })
  const importData = useMutation({
    mutationFn: (text: string) => api('/api/settings/import', { method: 'POST', body: text }),
    onMutate: () => setBackupMessage('导入中...'),
    onSuccess: () => {
      setBackupMessage('导入完成')
      setForm(null)
      void invalidateMonitor(qc)
      void qc.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (error) => setBackupMessage(errorMessage(error)),
  })
  async function exportData() {
    setBackupMessage('导出中...')
    try {
      const res = await fetch('/api/settings/export', { credentials: 'include' })
      if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || `HTTP ${res.status}`)
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `ai-upstream-monitor-${new Date().toISOString().slice(0, 10)}.json`
      a.click()
      URL.revokeObjectURL(url)
      setBackupMessage('已导出')
    } catch (error) {
      setBackupMessage(errorMessage(error))
    }
  }
  async function onImportFile(file?: File) {
    if (!file) return
    if (!window.confirm('导入会替换当前业务数据，继续吗？')) return
    importData.mutate(await file.text())
    if (fileInputRef.current) fileInputRef.current.value = ''
  }
  if (!data) return <ShellLoading />
  return (
    <Page title="设置" description="监控周期和 Telegram 告警">
      <Card className="max-w-2xl bg-card">
        <CardHeader>
          <CardTitle>基础设置</CardTitle>
          <CardDescription>探测模型由后端固定为 gpt-5.5</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Field label="站点名称">
            <Input value={data.site_name ?? ''} onChange={(e) => setForm({ ...data, site_name: e.target.value })} />
          </Field>
          <Field label="站点图标 URL">
            <Input value={data.site_icon ?? ''} placeholder="/favicon.ico 或 https://..." onChange={(e) => setForm({ ...data, site_icon: e.target.value })} />
          </Field>
          <Field label="检查间隔（分钟）">
            <Input type="number" value={data.check_interval_minutes} onChange={(e) => setForm({ ...data, check_interval_minutes: Number(e.target.value) })} />
          </Field>
          <Field label="Telegram Bot Token">
            <Input value={data.telegram_bot_token ?? ''} onChange={(e) => setForm({ ...data, telegram_bot_token: e.target.value })} />
          </Field>
          <Field label="Telegram Chat ID">
            <Input value={data.telegram_chat_id ?? ''} onChange={(e) => setForm({ ...data, telegram_chat_id: e.target.value })} />
          </Field>
          <Field label="探测模型">
            <Input value={data.probe_model} disabled />
          </Field>
          {message && <div className={cn('rounded-sm px-3 py-2 text-sm', save.isError ? 'bg-destructive/10 text-destructive' : 'bg-secondary text-muted-foreground')}>{message}</div>}
          <div>
            <Button onClick={() => save.mutate()} disabled={save.isPending}>
              {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
              保存
            </Button>
          </div>
        </CardContent>
      </Card>
      <Card className="max-w-2xl bg-card">
        <CardHeader>
          <CardTitle>数据备份</CardTitle>
          <CardDescription>导出和导入上游、Key、卡片、余额、检查记录和告警记录</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {backupMessage && (
            <div className={cn('rounded-sm px-3 py-2 text-sm', importData.isError ? 'bg-destructive/10 text-destructive' : 'bg-secondary text-muted-foreground')}>{backupMessage}</div>
          )}
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => void exportData()}>
              <Download className="size-4" />
              导出 JSON
            </Button>
            <Button variant="outline" onClick={() => fileInputRef.current?.click()} disabled={importData.isPending}>
              {importData.isPending ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />}
              导入 JSON
            </Button>
            <input ref={fileInputRef} className="hidden" type="file" accept="application/json,.json" onChange={(e) => void onImportFile(e.target.files?.[0])} />
          </div>
        </CardContent>
      </Card>
    </Page>
  )
}

function Action({ path, label }: { path: string; label: string }) {
  const qc = useQueryClient()
  const [message, setMessage] = useState('')
  const verb = label.replace(' Key', '')
  useAutoClear(message, `${verb}完成`, setMessage)
  const mutation = useMutation({
    mutationFn: () => api(path, { method: 'POST' }),
    onMutate: () => setMessage(`${verb}中...`),
    onSuccess: async () => {
      setMessage(`${verb}完成`)
      await invalidateMonitor(qc)
    },
    onError: (error) => setMessage(errorMessage(error)),
  })
  return (
    <div className="relative">
      <Button variant="outline" size="sm" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
        {mutation.isPending && <Loader2 className="size-3 animate-spin" />}
        {mutation.isPending ? `${verb}中` : label}
      </Button>
      {message && !mutation.isPending && <div className="absolute right-0 top-10 z-10 whitespace-nowrap rounded-sm border border-border bg-background px-2 py-1 text-xs text-muted-foreground">{message}</div>}
    </div>
  )
}

function IconAction({
  title,
  icon: Icon,
  onClick,
  pending,
  danger,
}: {
  title: string
  icon: ElementType
  onClick: () => void
  pending: boolean
  danger?: boolean
}) {
  return (
    <Button variant={danger ? 'danger' : 'outline'} size="icon" onClick={onClick} disabled={pending} title={title}>
      {pending ? <Loader2 className="size-4 animate-spin" /> : <Icon className="size-4" />}
      <span className="sr-only">{title}</span>
    </Button>
  )
}

function HoverText({ value, className, children }: { value?: string; className?: string; children?: ReactNode }) {
  const text = value || '-'
  return (
    <span className={cn('group relative block min-w-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30', className)} tabIndex={0}>
      {children ?? <span className="block truncate">{text}</span>}
      {text !== '-' && (
        <span className="pointer-events-none absolute bottom-full left-0 z-40 mb-2 hidden w-max min-w-56 max-w-[min(520px,calc(100vw-32px))] whitespace-pre-wrap break-words rounded-md border border-border bg-popover px-3 py-2 text-left text-xs leading-relaxed text-popover-foreground shadow-lg group-hover:block group-focus:block">
          {text}
        </span>
      )}
    </span>
  )
}

function openBrowserLogin(
  mutation: { mutate: (variables: void, options: { onSuccess: (out: { vnc_url: string }) => void; onError: (error: unknown) => void }) => void },
  onSuccess?: () => void,
  onError?: (error: unknown) => void,
) {
  const win = window.open(browserVNCURL, 'ai-upstream-monitor-vnc', 'popup=yes,width=1280,height=900')
  browserLoginWindow = win
  mutation.mutate(undefined, {
    onSuccess: () => onSuccess?.(),
    onError: (error) => {
      closeBrowserLoginWindow()
      if (onError) {
        onError(error)
      } else {
        window.alert(errorMessage(error))
      }
    },
  })
}

function WindowSelect({ value, setValue }: { value: string; setValue: (value: string) => void }) {
  return (
    <div className="flex overflow-hidden rounded-sm border border-border bg-background">
      {windows.map((item) => (
        <button
          key={item}
          className={cn('h-9 min-w-12 border-r border-border px-3 text-sm last:border-r-0', value === item ? 'bg-primary text-primary-foreground' : 'hover:bg-secondary')}
          onClick={() => setValue(item)}
        >
          {item}
        </button>
      ))}
    </div>
  )
}

function Page({
  title,
  description,
  actions,
  children,
}: {
  title: string
  description?: string
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="grid animate-in fade-in-50 slide-in-from-bottom-1 gap-4 duration-300">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="font-display text-4xl font-normal leading-tight">{title}</h1>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        {actions}
      </div>
      {children}
    </section>
  )
}

function Metric({ label, value, accent }: { label: string; value: ReactNode; accent?: 'success' | 'danger' }) {
  return (
    <Card className="gap-1.5 bg-card py-3">
      <CardContent className="px-3.5">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className={cn('font-display mt-0.5 text-2xl font-normal', accent === 'success' && 'text-success', accent === 'danger' && 'text-destructive')}>
          {value}
        </div>
      </CardContent>
    </Card>
  )
}

function MiniStat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-sm border border-border bg-background px-2.5 py-1.5">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate text-sm font-medium">{value}</div>
    </div>
  )
}

function EmptyPanel({ text }: { text: string }) {
  return (
    <Card className="bg-card">
      <CardContent className="py-12 text-center text-muted-foreground">{text}</CardContent>
    </Card>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-2">
      <label className="text-sm font-medium leading-none text-foreground">{label}</label>
      {children}
    </div>
  )
}

function EmptyRow({ colSpan, text }: { colSpan: number; text: string }) {
  return (
    <TableRow>
      <TableCell colSpan={colSpan} className="h-24 text-center text-muted-foreground">
        {text}
      </TableCell>
    </TableRow>
  )
}

function SkeletonRows({ cells, rows = 3 }: { cells: number; rows?: number }) {
  return Array.from({ length: rows }).map((_, row) => (
    <TableRow key={row}>
      {Array.from({ length: cells }).map((__, cell) => (
        <TableCell key={cell}>
          <Skeleton className="h-5 w-full min-w-16" />
        </TableCell>
      ))}
    </TableRow>
  ))
}

function SkeletonCardGrid({ count }: { count: number }) {
  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      {Array.from({ length: count }).map((_, index) => (
        <Card key={index} className="bg-card">
          <CardContent className="grid gap-4">
            <Skeleton className="h-6 w-2/3" />
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-16 w-full" />
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function probeStatus(probe: Probe) {
  return probe.status || (probe.success ? 'operational' : 'failed')
}

function probeOK(probe: Probe) {
  const status = probeStatus(probe)
  return status === 'operational' || status === 'degraded'
}

function probeStatusLabel(status: string) {
  return ({
    operational: '正常',
    degraded: '延迟偏高',
    validation_failed: '验证失败',
    failed: '请求失败',
    error: '探测错误',
  } as Record<string, string>)[status] || status || '-'
}

function probeHover(probe: Probe) {
  return [
    `状态：${probeStatusLabel(probeStatus(probe))}`,
    `延迟：${probe.latency_ms} ms`,
    `HTTP 状态：${probe.http_status || '-'}`,
    `检查时间：${fmtTime(probe.checked_at)}`,
    probe.error ? `详情：${probe.error}` : '',
    probe.output ? `验证题答案：${probe.output}` : '',
  ].filter(Boolean).join('\n')
}

function TypeBadge({ type }: { type: string }) {
  return <Badge variant="secondary">{type === 'newapi' ? 'new-api' : 'sub2api'}</Badge>
}

function StatusBadge({ ok, okText, failText }: { ok: boolean; okText: string; failText: string }) {
  return (
    <Badge variant={ok ? 'success' : 'destructive'}>
      {ok ? <CheckCircle2 className="size-3" /> : <XCircle className="size-3" />}
      {ok ? okText : failText}
    </Badge>
  )
}

function keysOf(row: UpstreamRow | undefined) {
  return row?.keys ?? []
}

function confirmDelete(name: string) {
  return window.confirm(`确认删除 ${name}？`)
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

function useAutoClear(value: string, targets: string, clear: (value: string) => void) {
  useEffect(() => {
    if (!targets.split('|').includes(value)) return
    const timer = window.setTimeout(() => clear(''), 1800)
    return () => window.clearTimeout(timer)
  }, [clear, targets, value])
}

function invalidateMonitor(qc: ReturnType<typeof useQueryClient>) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: ['upstreams'] }),
    qc.invalidateQueries({ queryKey: ['cards'] }),
    qc.invalidateQueries({ queryKey: ['balances'] }),
    qc.invalidateQueries({ queryKey: ['status'] }),
  ])
}

function num(value: number | undefined) {
  if (value === undefined || Number.isNaN(value)) return '-'
  return Number(value).toFixed(2)
}

function latestRefreshTime(rows: BalanceRow[]) {
  const latest = rows.reduce((max, row) => {
    const value = row.last_check ? new Date(row.last_check).getTime() : Number.NaN
    return Number.isNaN(value) || value <= max ? max : value
  }, 0)
  return latest ? new Date(latest).toISOString() : ''
}

function fmtTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', { hour12: false })
}
