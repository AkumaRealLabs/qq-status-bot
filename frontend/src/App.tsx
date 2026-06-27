import { useEffect, useMemo, useRef, useState, type ElementType, type ReactNode } from 'react'
import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { arrayMove, rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  CheckCircle2,
  ChevronRight,
  Copy,
  Database,
  Download,
  ExternalLink,
  GripVertical,
  KeyRound,
  Loader2,
  LogOut,
  MonitorCheck,
  Pencil,
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
  base_url?: string
  api_key?: string
  upstream_id?: string
  upstream_name: string
  key_id?: string
  key_name?: string
  key_group?: string
  key_group_ratio?: string
  effective_ratio?: string
  enabled: boolean
  public_enabled: boolean
  sort_order: number
  last_error: string
  history?: Probe[]
}

type PublicModelCard = Pick<ModelCard, 'name' | 'last_error' | 'history'>

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

type RechargeMethod = {
  type: string
  name: string
  min_amount?: number
  max_amount?: number
  external_url?: string
  direct: boolean
  sdk_only?: boolean
}

type RechargeCapabilities = {
  online_enabled: boolean
  redeem_enabled: boolean
  external_url?: string
  methods: RechargeMethod[]
}

type RechargeResult = {
  result_type: 'link' | 'qr' | 'sdk' | 'redeem' | 'order' | string
  payment_type: string
  remote_order_id?: string
  url?: string
  qr_code?: string
  message?: string
}

type RechargeLog = {
  id: string
  method: string
  amount: number
  payment_type: string
  remote_order_id: string
  status: string
  message: string
  created_at: string
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

type PublicMonitorStatus = Omit<MonitorStatus, 'rows'> & {
  rows: PublicModelCard[]
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
type CardForm = {
  name: string
  source: 'custom' | 'upstream'
  base_url: string
  api_key: string
  upstream_id: string
  key_id: string
  enabled: boolean
  public_enabled: boolean
}

const tabs: Array<{ id: TabID; label: string; short: string; icon: ElementType }> = [
  { id: 'status', label: '状态监控', short: '状态', icon: Activity },
  { id: 'balances', label: '余额监控', short: '余额', icon: WalletCards },
  { id: 'upstreams', label: '上游管理', short: '上游', icon: Database },
  { id: 'settings', label: '设置', short: '设置', icon: Settings },
]

const windows = ['1h', '3h', '5h', '1d', '7d', '15d']

const tabPaths: Record<TabID, string> = {
  status: '/admin/status',
  balances: '/admin/balances',
  upstreams: '/admin/upstreams',
  settings: '/admin/settings',
}

const emptyCardForm: CardForm = {
  name: '',
  source: 'custom',
  base_url: '',
  api_key: '',
  upstream_id: '',
  key_id: '',
  enabled: true,
  public_enabled: false,
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

function adminPath(pathname: string) {
  return pathname === '/admin' || pathname.startsWith('/admin/')
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
  const isAdmin = adminPath(location.pathname)
  const me = useQuery({ queryKey: ['me'], queryFn: () => api('/api/auth/me'), retry: false, enabled: setup.data?.initialized && isAdmin })
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

  if (setup.isLoading) return <ShellLoading />
  if (!setup.data?.initialized) {
    if (!isAdmin) return <PublicStatusPage site={site} />
    return <SetupPage site={site} onDone={() => void setup.refetch()} />
  }
  if (!isAdmin) return <PublicStatusPage site={site} />
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

      <div className="min-w-0 lg:pl-64">
        <header className="sticky top-0 z-20 flex h-16 items-center justify-between border-b border-border bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80 lg:px-6">
          <div className="min-w-0">
            <div className="text-base font-medium text-foreground">{active.label}</div>
          </div>
          <Button variant="outline" size="sm" className="lg:hidden" onClick={logout} disabled={loggingOut}>
            {loggingOut ? <Loader2 className="size-4 animate-spin" /> : <LogOut className="size-4" />}
            退出
          </Button>
        </header>

        <main className="mx-auto grid w-full max-w-[1200px] min-w-0 animate-in gap-4 p-4 fade-in-50 duration-300 lg:p-6">
          <MobileTabs tab={tab} setTab={navigate} />
          {tab === 'status' && <AdminStatusPage />}
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
    <div className="mb-4 grid min-w-0 grid-cols-4 gap-2 md:hidden">
      {tabs.map((item) => (
        <Button key={item.id} variant={tab === item.id ? 'secondary' : 'outline'} size="sm" className="px-2" onClick={() => setTab(item.id)}>
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
  const [username, setUsername] = useState('')
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
      <form className="grid gap-4" autoComplete="off" onSubmit={submit}>
        <FormError error={mutation.error} />
        <Field label="用户名">
          <Input name="monitor-setup-user" autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="密码">
          <Input name="monitor-setup-pass" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
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
  const [username, setUsername] = useState('')
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
      <form className="grid gap-4" autoComplete="off" onSubmit={submit}>
        <FormError error={mutation.error} />
        <Field label="用户名">
          <Input name="monitor-login-user" autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="密码">
          <Input name="monitor-login-pass" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
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

function PublicStatusPage({ site }: { site?: SiteSettings }) {
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
          <div className="flex min-w-0 items-center gap-3">
            <BrandIcon src={site?.site_icon} />
            <div className="truncate font-display text-xl font-normal leading-none text-foreground">{siteName}</div>
          </div>
          <Button variant="outline" size="sm" onClick={() => (location.href = '/admin/status')}>后台</Button>
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
            <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
              {cards.map((card, index) => <StatusMonitorCard key={`${card.name}-${index}`} card={card} windowValue={windowValue} publicView />)}
            </div>
          )}
          {!q.isLoading && cards.length === 0 && <EmptyPanel text="暂无公开卡片" />}
        </Page>
      </main>
    </div>
  )
}

function AdminStatusPage() {
  const qc = useQueryClient()
  const [windowValue, setWindowValue] = useState('1h')
  const [layoutEditing, setLayoutEditing] = useState(false)
  const [draftCards, setDraftCards] = useState<ModelCard[]>([])
  const q = useQuery({
    queryKey: ['status', windowValue],
    queryFn: () => api<MonitorStatus>(`/api/monitor/status?window=${windowValue}`),
    refetchInterval: 60000,
  })
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamRow[]>('/api/upstreams') })
  const cards = q.data?.rows ?? []
  const shownCards = layoutEditing ? draftCards : cards
  const sortDirty = !sameIDs(cards, draftCards)
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
  const sortCards = useMutation({
    mutationFn: (ids: string[]) => api('/api/cards/order', { method: 'POST', body: JSON.stringify({ ids }) }),
    onSuccess: async () => {
      await Promise.all([qc.invalidateQueries({ queryKey: ['status'] }), qc.invalidateQueries({ queryKey: ['cards'] })])
    },
    onError: (error) => window.alert(errorMessage(error)),
  })
  useEffect(() => {
    if (!layoutEditing) setDraftCards(cards)
  }, [q.data?.rows, layoutEditing])
  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event
    if (!over || active.id === over.id) return
    const oldIndex = draftCards.findIndex((card) => card.id === active.id)
    const newIndex = draftCards.findIndex((card) => card.id === over.id)
    if (oldIndex < 0 || newIndex < 0) return
    setDraftCards((value) => arrayMove(value, oldIndex, newIndex))
  }
  const saveSort = () => {
    sortCards.mutate(draftCards.map((card) => card.id).filter(Boolean) as string[], { onSuccess: () => setLayoutEditing(false) })
  }
  return (
    <Page
      title="状态监控"
      description="探测结果、公开展示和状态卡片"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {(q.isFetching || upstreams.isFetching) && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <WindowSelect value={windowValue} setValue={setWindowValue} />
          {layoutEditing ? (
            <>
              <Button variant="outline" size="sm" onClick={() => { setDraftCards(cards); setLayoutEditing(false) }} disabled={sortCards.isPending}>取消</Button>
              <Button size="sm" onClick={saveSort} disabled={!sortDirty || sortCards.isPending}>
                {sortCards.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                保存排序
              </Button>
            </>
          ) : (
            cards.length > 0 && <Button variant="outline" size="sm" onClick={() => { setDraftCards(cards); setLayoutEditing(true) }}>修改布局</Button>
          )}
          <CardDialog rows={upstreams.data ?? []} />
        </div>
      }
    >
      <StatusSummary data={q.data} />
      {q.isLoading && <SkeletonCardGrid count={6} />}
      {!q.isLoading && shownCards.length > 0 && (
        layoutEditing ? (
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={shownCards.map((card) => card.id)} strategy={rectSortingStrategy}>
              <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
                {shownCards.map((card) => <SortableStatusCard key={card.id} card={card} windowValue={windowValue} rows={upstreams.data ?? []} sorting={sortCards.isPending} />)}
              </div>
            </SortableContext>
          </DndContext>
        ) : (
          <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
            {shownCards.map((card) => <StatusMonitorCard key={card.id} card={card} windowValue={windowValue} rows={upstreams.data ?? []} />)}
          </div>
        )
      )}
      {!q.isLoading && cards.length === 0 && <EmptyPanel text="暂无卡片" />}
    </Page>
  )
}

function StatusSummary({ data }: { data?: Pick<MonitorStatus, 'requests' | 'success' | 'failed' | 'success_rate' | 'avg_latency'> }) {
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

function SortableStatusCard({
  card,
  windowValue,
  rows,
  sorting,
}: {
  card: ModelCard
  windowValue: string
  rows: UpstreamRow[]
  sorting: boolean
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: card.id })
  return (
    <div
      ref={setNodeRef}
      className={cn('min-w-0', isDragging && 'z-10 opacity-80')}
      style={{ transform: CSS.Transform.toString(transform), transition }}
    >
      <StatusMonitorCard
        card={card}
        windowValue={windowValue}
        rows={rows}
        dragHandle={
          <Button
            variant="outline"
            size="icon"
            title="拖拽排序"
            disabled={sorting}
            className="touch-none"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="size-4" />
            <span className="sr-only">拖拽排序</span>
          </Button>
        }
      />
    </div>
  )
}

function StatusMonitorCard({
  card,
  windowValue,
  rows = [],
  publicView,
  dragHandle,
}: {
  card: ModelCard | PublicModelCard
  windowValue: string
  rows?: UpstreamRow[]
  publicView?: boolean
  dragHandle?: ReactNode
}) {
  const qc = useQueryClient()
  const [message, setMessage] = useState('')
  useEffect(() => {
    if (message !== '检查完成') return
    const timer = window.setTimeout(() => setMessage(''), 1800)
    return () => window.clearTimeout(timer)
  }, [message])
  const editableCard = isModelCard(card) ? card : undefined
  const check = useMutation({
    mutationFn: () => api(`/api/cards/${editableCard?.id ?? ''}/check`, { method: 'POST' }),
    onMutate: () => setMessage('检查中...'),
    onSuccess: async () => {
      setMessage('检查完成')
      await qc.invalidateQueries({ queryKey: ['status'] })
    },
    onError: (error) => setMessage(errorMessage(error)),
  })
  const history = card.history ?? []
  const latest = history.at(-1)
  const ok = latest ? probeOK(latest) : !card.last_error
  const statusText = latest ? probeStatusLabel(probeStatus(latest)) : ok ? probeStatusLabel('operational') : probeStatusLabel('failed')
  const successCount = history.filter(probeOK).length
  const uptime = history.length ? `${((successCount / history.length) * 100).toFixed(2)}%` : '-'
  const groupName = editableCard?.key_group || (editableCard?.base_url ? '自定义' : '-')
  const ratio = editableCard?.effective_ratio || editableCard?.key_group_ratio || '-'
  return (
    <Card className={cn('min-w-0 bg-card', !ok && 'border-destructive/40')}>
      <CardHeader className="min-h-16 gap-2 border-b border-border">
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
          <div className="min-w-0 pt-1">
            <CardTitle className="break-words text-lg leading-tight">{cardTitle(card, ratio)}</CardTitle>
            {!publicView && (
              <CardDescription className="mt-1.5 grid gap-0.5 text-xs leading-relaxed">
                <span>分组：{groupName}</span>
                <span>模型：gpt-5.5</span>
              </CardDescription>
            )}
          </div>
          <div className="flex shrink-0 flex-col items-end gap-1.5">
            <StatusBadge ok={ok} okText={statusText} failText={statusText} />
            {!publicView && editableCard && (
              <div className="flex flex-wrap justify-end gap-1.5">
                {dragHandle}
                <CardDialog rows={rows} card={editableCard} />
                <Button variant="outline" size="icon" onClick={() => check.mutate()} disabled={check.isPending} title="检查">
                  {check.isPending ? <Loader2 className="size-3 animate-spin" /> : <RefreshCcw className="size-3" />}
                  <span className="sr-only">检查</span>
                </Button>
              </div>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent className="grid gap-2.5 pt-2.5">
        <div className="grid min-w-0 grid-cols-2 gap-2">
          <MiniStat label="对话延迟" value={latest ? `${latest.latency_ms} ms` : '-'} />
          <MiniStat label="端点 PING" value={latest?.http_status ? String(latest.http_status) : '-'} />
        </div>
        <div className="min-w-0 border-t border-border pt-2.5">
          <div className="mb-2 flex items-end justify-between gap-2">
            <div className="text-xs text-muted-foreground">可用性 · {windowValue}</div>
            <div className={cn('font-display text-2xl font-normal', ok ? 'text-success' : 'text-destructive')}>{uptime}</div>
          </div>
          <div className="-mx-1 overflow-x-auto px-1 pb-1">
            <div
              className="grid min-w-full gap-1"
              style={{ gridTemplateColumns: `repeat(${history.length || emptyHistorySlots(windowValue)}, minmax(6px, 1fr))` }}
            >
              {history.map((probe, index) => {
                const good = probeOK(probe)
                return (
                  <HoverText
                    key={`${probe.checked_at}-${index}`}
                    value={probeHoverTitle(probe)}
                    content={<ProbeTooltip probe={probe} />}
                    nativeTitle={false}
                    className={cn('h-4 rounded-sm', good ? 'bg-success' : 'bg-destructive')}
                  >
                    <span className="sr-only">{probeStatus(probe)}</span>
                  </HoverText>
                )
              })}
              {history.length === 0 &&
                Array.from({ length: emptyHistorySlots(windowValue) }).map((_, index) => <span key={index} className="h-4 rounded-sm bg-surface-cream-strong" />)}
            </div>
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
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {lastRefresh && <span className="min-w-0 text-sm text-muted-foreground">最后刷新：{fmtTime(lastRefresh)}</span>}
          {message && <span className={cn('min-w-0 text-sm', refresh.isError ? 'text-destructive' : 'text-muted-foreground')}>{message}</span>}
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
      </CardContent>
    </Card>
  )
}

function UpstreamsPage() {
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamRow[]>('/api/upstreams') })
  const upstreamRows = upstreams.data ?? []
  return (
    <Page
      title="上游管理"
      description="上游凭据、Key 同步和余额相关能力"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {upstreams.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <UpstreamDialog />
        </div>
      }
    >
      <Card className="min-w-0">
        <CardHeader>
          <CardTitle>上游</CardTitle>
          <CardDescription>new-api 与 sub2api 普通用户接入</CardDescription>
        </CardHeader>
        <CardContent className="min-w-0">
          <div className="grid min-w-0 gap-3 md:hidden">
            {upstreams.isLoading && <div className="rounded-sm border border-border bg-background p-3 text-sm text-muted-foreground">加载中...</div>}
            {!upstreams.isLoading && upstreamRows.map((row) => <UpstreamMobileItem key={row.upstream.id} row={row} />)}
            {!upstreams.isLoading && upstreamRows.length === 0 && <div className="rounded-sm border border-border bg-background p-3 text-sm text-muted-foreground">暂无上游</div>}
          </div>
          <div className="hidden min-w-0 md:block">
            <Table className="min-w-[960px] table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-36">名称</TableHead>
                  <TableHead className="w-24">类型</TableHead>
                  <TableHead className="w-72">地址</TableHead>
                  <TableHead className="w-16">Key</TableHead>
                  <TableHead className="w-24">余额</TableHead>
                  <TableHead className="w-24">状态</TableHead>
                  <TableHead className="w-56">错误</TableHead>
                  <TableHead className="w-72 text-right">操作</TableHead>
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
    </Page>
  )
}

function UpstreamTableRow({ row }: { row: UpstreamRow }) {
  const upstream = row.upstream
  const error = upstream.last_error || row.balance?.error || '-'
  return (
    <TableRow>
      <TableCell className="font-medium">
        <HoverText value={upstream.name} />
      </TableCell>
      <TableCell>
        <TypeBadge type={upstream.type} />
      </TableCell>
      <TableCell>
        <HoverText value={upstream.base_url} />
      </TableCell>
      <TableCell>{keysOf(row).length}</TableCell>
      <TableCell>{num(row.balance?.remain)}</TableCell>
      <TableCell>
        <StatusBadge ok={upstream.enabled} okText="启用" failText="停用" />
      </TableCell>
      <TableCell>
        <HoverText value={error} className={error === '-' ? '' : 'text-destructive'} />
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
    <div className="grid min-w-0 gap-3 rounded-sm border border-border bg-background p-3">
      <div className="flex min-w-0 items-start justify-between gap-3">
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
    <div className={cn('flex max-w-full flex-wrap gap-2', mobile ? '' : 'justify-end')}>
      <UpstreamDialog upstream={upstream} />
      <BalanceRechargeDialog upstream={upstream} />
      <Action path={`/api/upstreams/${upstream.id}/sync-keys`} label="同步 Key" />
      <Action path={`/api/upstreams/${upstream.id}/check`} label="检查" />
      <IconAction title="删除" onClick={() => confirmDelete(upstream.name) && remove.mutate()} pending={remove.isPending} icon={Trash2} danger />
    </div>
  )
}

function BalanceRechargeDialog({ upstream }: { upstream: Upstream }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [amount, setAmount] = useState('')
  const [paymentType, setPaymentType] = useState('')
  const [code, setCode] = useState('')
  const [result, setResult] = useState<RechargeResult | null>(null)
  const caps = useQuery({
    queryKey: ['balance-recharge-capabilities', upstream.id],
    queryFn: () => api<RechargeCapabilities>(`/api/upstreams/${upstream.id}/balance-recharge/capabilities`),
    enabled: open,
  })
  const logs = useQuery({
    queryKey: ['balance-recharge-logs', upstream.id],
    queryFn: () => api<RechargeLog[]>(`/api/upstreams/${upstream.id}/balance-recharge/logs`),
    enabled: open,
  })
  const methods = caps.data?.methods ?? []
  const selectedMethod = methods.find((item) => item.type === paymentType) ?? methods[0]
  useEffect(() => {
    if (open && !paymentType && methods[0]) setPaymentType(methods[0].type)
  }, [methods, open, paymentType])
  const refreshAfterSubmit = async () => {
    await Promise.all([
      invalidateMonitor(qc),
      qc.invalidateQueries({ queryKey: ['balance-recharge-logs', upstream.id] }),
    ])
  }
  const createOrder = useMutation({
    mutationFn: () => api<RechargeResult>(`/api/upstreams/${upstream.id}/balance-recharge/order`, {
      method: 'POST',
      body: JSON.stringify({ amount: Number(amount), payment_type: paymentType }),
    }),
    onSuccess: async (out) => {
      setResult(out)
      await refreshAfterSubmit()
    },
  })
  const redeem = useMutation({
    mutationFn: () => api<RechargeResult>(`/api/upstreams/${upstream.id}/balance-recharge/redeem`, {
      method: 'POST',
      body: JSON.stringify({ code }),
    }),
    onSuccess: async (out) => {
      setResult(out)
      setCode('')
      await refreshAfterSubmit()
    },
  })
  const busy = caps.isFetching || createOrder.isPending || redeem.isPending
  const unavailable = caps.data && !caps.data.online_enabled && !caps.data.redeem_enabled && !caps.data.external_url
  const amountRequired = !paymentType.startsWith('creem:')
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) setResult(null)
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <WalletCards className="size-4" />
          余额充值
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>余额充值 · {upstream.name}</DialogTitle>
        {caps.isLoading && <div className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />探测充值能力...</div>}
        <FormError error={caps.error || createOrder.error || redeem.error} />
        {unavailable && <EmptyPanel text="该站点未开放余额充值或兑换码" />}
        {caps.data?.external_url && (
          <div className="rounded-sm border border-border bg-secondary/50 p-3">
            <Button asChild variant="outline" size="sm">
              <a href={caps.data.external_url} target="_blank" rel="noreferrer">
                <ExternalLink className="size-4" />
                打开上游充值页
              </a>
            </Button>
          </div>
        )}
        {caps.data?.online_enabled && methods.length > 0 && (
          <div className="grid min-w-0 gap-4 rounded-sm border border-border bg-card p-3 md:grid-cols-2">
            <Field label="金额">
              <Input type="number" min={selectedMethod?.min_amount || 0} max={selectedMethod?.max_amount || undefined} value={amount} onChange={(e) => setAmount(e.target.value)} />
            </Field>
            <Field label="支付方式">
              <Select value={paymentType} onValueChange={setPaymentType}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {methods.map((method) => (
                    <SelectItem key={method.type} value={method.type}>{method.name || method.type}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <div className="md:col-span-2">
              <Button onClick={() => createOrder.mutate()} disabled={busy || (amountRequired && !Number(amount)) || !paymentType}>
                {createOrder.isPending ? <Loader2 className="size-4 animate-spin" /> : <WalletCards className="size-4" />}
                创建订单
              </Button>
            </div>
          </div>
        )}
        {caps.data?.redeem_enabled && (
          <div className="grid min-w-0 gap-4 rounded-sm border border-border bg-card p-3">
            <Field label="兑换码">
              <Input value={code} onChange={(e) => setCode(e.target.value)} />
            </Field>
            <div>
              <Button variant="outline" onClick={() => redeem.mutate()} disabled={busy || !code.trim()}>
                {redeem.isPending ? <Loader2 className="size-4 animate-spin" /> : <KeyRound className="size-4" />}
                提交兑换码
              </Button>
            </div>
          </div>
        )}
        {result && <RechargeResultPanel result={result} />}
        <RechargeLogs logs={logs.data ?? []} loading={logs.isLoading} />
      </DialogContent>
    </Dialog>
  )
}

function RechargeResultPanel({ result }: { result: RechargeResult }) {
  const link = result.url
  const qr = result.qr_code
  return (
    <div className="grid min-w-0 gap-3 rounded-sm border border-border bg-secondary/50 p-3">
      <div className="text-sm font-medium text-foreground">提交成功</div>
      {link && (
        <div className="flex min-w-0 flex-wrap gap-2">
          <Button asChild size="sm">
            <a href={link} target="_blank" rel="noreferrer">
              <ExternalLink className="size-4" />
              打开支付页
            </a>
          </Button>
          <CopyButton text={link} />
        </div>
      )}
      {qr && (
        <div className="grid min-w-0 gap-2">
          <div className="break-all rounded-sm border border-border bg-background p-2 font-mono text-xs">{qr}</div>
          <CopyButton text={qr} label="复制二维码内容" />
        </div>
      )}
      {!link && !qr && <div className="text-sm text-muted-foreground">{result.message || '该支付方式需要去上游站点完成'}</div>}
    </div>
  )
}

function CopyButton({ text, label = '复制链接' }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  useAutoClear(copied ? '已复制' : '', '已复制', () => setCopied(false))
  return (
    <Button
      variant="outline"
      size="sm"
      onClick={() => void navigator.clipboard.writeText(text).then(() => setCopied(true))}
    >
      <Copy className="size-4" />
      {copied ? '已复制' : label}
    </Button>
  )
}

function RechargeLogs({ logs, loading }: { logs: RechargeLog[]; loading: boolean }) {
  if (loading) return <div className="text-sm text-muted-foreground">加载记录...</div>
  if (logs.length === 0) return null
  return (
    <div className="grid min-w-0 gap-2">
      <div className="text-sm font-medium text-foreground">操作记录</div>
      <div className="grid max-h-52 gap-2 overflow-auto">
        {logs.map((log) => (
          <div key={log.id} className="grid min-w-0 gap-1 rounded-sm border border-border bg-background p-2 text-xs">
            <div className="flex min-w-0 justify-between gap-2">
              <span>{log.method === 'redeem' ? '兑换码' : log.payment_type}</span>
              <span className={log.status === 'success' ? 'text-success' : 'text-destructive'}>{log.status === 'success' ? '成功' : '失败'}</span>
            </div>
            <div className="text-muted-foreground">{fmtTime(log.created_at)} · {num(log.amount)}</div>
            {log.message && <HoverText value={log.message} className="text-muted-foreground" />}
          </div>
        ))}
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
        <div className="grid min-w-0 gap-4 md:grid-cols-2">
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

function CardDialog({ rows, card }: { rows: UpstreamRow[]; card?: ModelCard }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<CardForm>(() => cardToForm(card))
  const keys = useMemo(() => keysOf(rows.find((row) => row.upstream.id === form.upstream_id)), [rows, form.upstream_id])
  const save = useMutation({
    mutationFn: () =>
      api(card ? `/api/cards/${card.id}` : '/api/cards', {
        method: card ? 'PATCH' : 'POST',
        body: JSON.stringify(cardPayload(form)),
      }),
    onSuccess: () => {
      setOpen(false)
      void invalidateMonitor(qc)
    },
  })
  const remove = useMutation({
    mutationFn: () => api(`/api/cards/${card?.id ?? ''}`, { method: 'DELETE' }),
    onSuccess: () => {
      setOpen(false)
      void invalidateMonitor(qc)
    },
    onError: (error) => window.alert(errorMessage(error)),
  })
  const update = (patch: Partial<CardForm>) => setForm((value) => ({ ...value, ...patch }))
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) setForm(cardToForm(card))
      }}
    >
      <DialogTrigger asChild>
        {card ? (
          <Button variant="outline" size="icon" title="编辑">
            <Pencil className="size-4" />
            <span className="sr-only">编辑</span>
          </Button>
        ) : (
          <Button size="sm">
            <KeyRound className="size-4" />
            新增卡片
          </Button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>{card ? '编辑状态卡片' : '新增状态卡片'}</DialogTitle>
        <FormError error={save.error} />
        <div className="grid min-w-0 gap-4 md:grid-cols-2">
          <Field label="名称">
            <Input value={form.name} onChange={(e) => update({ name: e.target.value })} />
          </Field>
          <Field label="来源模式">
            <Select value={form.source} onValueChange={(value) => update({ source: value as CardForm['source'], key_id: value === 'custom' ? '' : form.key_id })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="custom">自定义</SelectItem>
                <SelectItem value="upstream">选择上游 Key</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {form.source === 'custom' ? (
            <>
              <Field label="Base URL">
                <Input value={form.base_url} onChange={(e) => update({ base_url: e.target.value })} />
              </Field>
              <Field label="Key">
                <Input value={form.api_key} onChange={(e) => update({ api_key: e.target.value })} />
              </Field>
            </>
          ) : (
            <>
              <Field label="上游">
                <Select
                  value={form.upstream_id}
                  onValueChange={(value) => update({ upstream_id: value, key_id: '' })}
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
                <Select value={form.key_id} onValueChange={(value) => update({ key_id: value })}>
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
            </>
          )}
          <Field label="自动探测">
            <Select value={form.enabled ? 'true' : 'false'} onValueChange={(value) => update({ enabled: value === 'true' })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="true">参与自动探测</SelectItem>
                <SelectItem value="false">暂停自动探测</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="展示给游客">
            <Select value={form.public_enabled ? 'true' : 'false'} onValueChange={(value) => update({ public_enabled: value === 'true' })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="false">不展示</SelectItem>
                <SelectItem value="true">展示</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="固定模型">
            <Input value="gpt-5.5" disabled />
          </Field>
        </div>
        <div className="flex flex-wrap justify-between gap-2">
          {card ? <IconAction title="删除" onClick={() => confirmDelete(card.name, '只删除卡片，历史探测记录会保留。') && remove.mutate()} pending={remove.isPending} icon={Trash2} danger /> : <span />}
          <Button onClick={() => save.mutate()} disabled={save.isPending || !cardFormReady(form)}>
            {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
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
      <Card className="w-full max-w-2xl bg-card">
        <CardHeader>
          <CardTitle>基础设置</CardTitle>
          <CardDescription>探测模型由后端固定为 gpt-5.5</CardDescription>
        </CardHeader>
        <CardContent className="grid min-w-0 gap-4">
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
      <Card className="w-full max-w-2xl bg-card">
        <CardHeader>
          <CardTitle>数据备份</CardTitle>
          <CardDescription>导出和导入上游、Key、卡片、余额、检查记录和告警记录</CardDescription>
        </CardHeader>
        <CardContent className="grid min-w-0 gap-4">
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
    <div className="relative min-w-0">
      <Button variant="outline" size="sm" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
        {mutation.isPending && <Loader2 className="size-3 animate-spin" />}
        {mutation.isPending ? `${verb}中` : label}
      </Button>
      {message && !mutation.isPending && <div className="absolute right-0 top-10 z-10 max-w-[calc(100vw-32px)] whitespace-nowrap rounded-sm border border-border bg-background px-2 py-1 text-xs text-muted-foreground">{message}</div>}
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

function HoverText({
  value,
  className,
  children,
  content,
  nativeTitle = true,
}: {
  value?: string
  className?: string
  children?: ReactNode
  content?: ReactNode
  nativeTitle?: boolean
}) {
  const text = value || '-'
  const [tooltipStyle, setTooltipStyle] = useState<React.CSSProperties>()
  const placeTooltip = (target: HTMLElement) => {
    const rect = target.getBoundingClientRect()
    const width = Math.min(520, window.innerWidth - 32)
    const left = Math.min(Math.max(16, rect.left), window.innerWidth - width - 16)
    setTooltipStyle({ left, top: Math.max(12, rect.top - 12), transform: 'translateY(-100%)' })
  }
  return (
    <span
      className={cn('group relative block min-w-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30', className)}
      tabIndex={0}
      title={nativeTitle ? text : undefined}
      onMouseEnter={(event) => placeTooltip(event.currentTarget)}
      onFocus={(event) => placeTooltip(event.currentTarget)}
    >
      {children ?? <span className="block truncate">{text}</span>}
      {text !== '-' && (
        <span
          className="pointer-events-none fixed z-50 hidden w-max min-w-56 max-w-[min(520px,calc(100vw-32px))] rounded-md border border-border bg-popover text-left text-popover-foreground shadow-lg group-hover:block group-focus:block"
          style={tooltipStyle}
        >
          {content ?? <span className="block whitespace-pre-wrap break-words px-3 py-2 text-sm leading-[1.55]">{text}</span>}
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
    <div className="max-w-full overflow-x-auto rounded-sm border border-border bg-background">
      <div className="flex min-w-max">
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
    <section className="grid min-w-0 animate-in fade-in-50 slide-in-from-bottom-1 gap-4 duration-300">
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h1 className="break-words font-display text-3xl font-normal leading-tight sm:text-4xl">{title}</h1>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="min-w-0 sm:shrink-0">{actions}</div>}
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
        <div className={cn('font-display mt-0.5 break-words text-2xl font-normal', accent === 'success' && 'text-success', accent === 'danger' && 'text-destructive')}>
          {value}
        </div>
      </CardContent>
    </Card>
  )
}

function MiniStat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="min-w-0 rounded-sm border border-border bg-background px-2.5 py-1.5">
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
    <div className="grid min-w-0 gap-4 md:grid-cols-2 xl:grid-cols-3">
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
  return ['operational', 'degraded', '正常', '延迟偏高'].includes(status)
}

function emptyHistorySlots(windowValue: string) {
  return ({ '1h': 12, '3h': 18, '5h': 20, '1d': 24, '7d': 28, '15d': 30 } as Record<string, number>)[windowValue] || 24
}

function probeStatusLabel(status: string) {
  return ({
    operational: '正常',
    degraded: '延迟偏高',
    validation_failed: '验证失败',
    failed: '请求失败',
    error: '探测异常',
    正常: '正常',
    延迟偏高: '延迟偏高',
    验证失败: '验证失败',
    请求失败: '请求失败',
    探测异常: '探测异常',
  } as Record<string, string>)[status] || status || '-'
}

function ProbeTooltip({ probe }: { probe: Probe }) {
  const ok = probeOK(probe)
  const rows = [
    ['状态', probeStatusLabel(probeStatus(probe)), ok ? 'text-success' : 'text-destructive'],
    ['延迟', `${probe.latency_ms} ms`],
    ['HTTP 状态', probe.http_status || '-'],
    ['模型验证', ok ? '通过' : '未通过', ok ? 'text-success' : 'text-destructive'],
    ['检查时间', fmtTime(probe.checked_at)],
    probe.error ? ['详情', probe.error, 'text-destructive'] : undefined,
  ].filter(Boolean) as string[][]
  return (
    <span className="block min-w-64 max-w-[min(520px,calc(100vw-32px))] rounded-md bg-popover px-3 py-3 text-sm leading-[1.55] text-popover-foreground">
      <span className="mb-2 block border-b border-hairline-soft pb-2 text-[13px] font-medium leading-[1.4] text-muted-foreground">探测详情</span>
      <span className="grid gap-1.5">
        {rows.map(([label, value, tone]) => (
          <span key={label} className="grid grid-cols-[72px_minmax(0,1fr)] gap-3">
            <span className="whitespace-nowrap text-[13px] font-medium leading-[1.4] text-muted-foreground">{label}</span>
            <span className={cn('break-words text-sm leading-[1.55] text-foreground', tone)}>{value}</span>
          </span>
        ))}
      </span>
    </span>
  )
}

function probeHoverTitle(probe: Probe) {
  const ok = probeOK(probe)
  return `状态：${probeStatusLabel(probeStatus(probe))}\n延迟：${probe.latency_ms} ms\nHTTP 状态：${probe.http_status || '-'}\n模型验证：${ok ? '通过' : '未通过'}\n检查时间：${fmtTime(probe.checked_at)}`
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

function sameIDs(a: ModelCard[], b: ModelCard[]) {
  return a.length === b.length && a.every((item, index) => item.id === b[index]?.id)
}

function isModelCard(card: ModelCard | PublicModelCard): card is ModelCard {
  return 'id' in card
}

function cardTitle(card: ModelCard | PublicModelCard, ratio: string) {
  if (!isModelCard(card)) return card.name
  if (card.base_url) return card.name
  const detail = ratio !== '-' ? ratio : card.key_name || card.key_group || ''
  return detail ? `${card.upstream_name || card.name} · ${detail}` : card.name
}

function cardToForm(card?: ModelCard): CardForm {
  if (!card) return emptyCardForm
  const custom = Boolean(card.base_url)
  return {
    name: card.name || '',
    source: custom ? 'custom' : 'upstream',
    base_url: card.base_url || '',
    api_key: card.api_key || '',
    upstream_id: card.upstream_id || '',
    key_id: card.key_id || '',
    enabled: card.enabled,
    public_enabled: card.public_enabled,
  }
}

function cardPayload(form: CardForm) {
  return {
    name: form.name,
    base_url: form.source === 'custom' ? form.base_url : '',
    api_key: form.source === 'custom' ? form.api_key : '',
    upstream_id: form.source === 'upstream' ? form.upstream_id : '',
    key_id: form.source === 'upstream' ? form.key_id : '',
    enabled: form.enabled,
    public_enabled: form.public_enabled,
  }
}

function cardFormReady(form: CardForm) {
  if (!form.name.trim()) return false
  return form.source === 'custom' ? Boolean(form.base_url.trim() && form.api_key.trim()) : Boolean(form.upstream_id && form.key_id)
}

function confirmDelete(name: string, note?: string) {
  return window.confirm(`确认删除 ${name}？${note ? `\n${note}` : ''}`)
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
