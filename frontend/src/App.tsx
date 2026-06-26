import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Database, KeyRound, LogOut, Plus, RefreshCcw, Save, Settings, WalletCards } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

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

type Card = {
  id: string
  name: string
  upstream_id: string
  upstream_name: string
  key_id: string
  key_name: string
  key_group: string
  key_group_ratio: string
  enabled: boolean
  last_error: string
  history?: Probe[]
}

type Probe = {
  checked_at: string
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
  rows: Card[]
}

type SettingsData = {
  check_interval_minutes: number
  telegram_bot_token?: string
  telegram_chat_id?: string
  probe_model: string
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

const emptyUpstream: Upstream = {
  id: '',
  name: '',
  type: 'newapi',
  base_url: '',
  enabled: true,
  balance_rate: 1,
  low_balance_threshold: 0,
}

export default function App() {
  const qc = useQueryClient()
  const [tab, setTab] = useState('status')
  const setup = useQuery({ queryKey: ['setup'], queryFn: () => api<{ initialized: boolean }>('/api/setup/status') })
  const me = useQuery({ queryKey: ['me'], queryFn: () => api('/api/auth/me'), retry: false, enabled: setup.data?.initialized })

  if (setup.isLoading) return <ShellLoading />
  if (!setup.data?.initialized) return <SetupPage onDone={() => void setup.refetch()} />
  if (me.isError) return <LoginPage onDone={() => void me.refetch()} />

  const logout = async () => {
    await api('/api/auth/logout', { method: 'POST' })
    qc.clear()
    location.reload()
  }

  return (
    <div className="min-h-screen bg-[#f6f7f9]">
      <aside className="fixed inset-y-0 left-0 hidden w-60 border-r border-zinc-200 bg-white lg:block">
        <div className="flex h-14 items-center border-b border-zinc-200 px-5 font-semibold">AI 上游监控</div>
        <nav className="grid gap-1 p-3">
          <Nav id="status" tab={tab} setTab={setTab} icon={<Activity className="size-4" />} text="状态监控" />
          <Nav id="balances" tab={tab} setTab={setTab} icon={<WalletCards className="size-4" />} text="余额监控" />
          <Nav id="upstreams" tab={tab} setTab={setTab} icon={<Database className="size-4" />} text="上游管理" />
          <Nav id="settings" tab={tab} setTab={setTab} icon={<Settings className="size-4" />} text="设置" />
        </nav>
      </aside>
      <main className="lg:pl-60">
        <header className="sticky top-0 z-10 flex h-14 items-center justify-between border-b border-zinc-200 bg-white/90 px-4 backdrop-blur lg:px-6">
          <div className="flex gap-2 lg:hidden">
            {['status', 'balances', 'upstreams', 'settings'].map((id) => (
              <Button key={id} variant={tab === id ? 'default' : 'outline'} size="sm" onClick={() => setTab(id)}>
                {id === 'status' ? '状态' : id === 'balances' ? '余额' : id === 'upstreams' ? '上游' : '设置'}
              </Button>
            ))}
          </div>
          <div className="hidden text-sm text-zinc-500 lg:block">探测模型固定为 gpt-5.5</div>
          <Button variant="ghost" onClick={logout}>
            <LogOut className="size-4" /> 退出
          </Button>
        </header>
        <section className="p-4 lg:p-6">
          {tab === 'status' && <StatusPage />}
          {tab === 'balances' && <BalancesPage />}
          {tab === 'upstreams' && <UpstreamsPage />}
          {tab === 'settings' && <SettingsPage />}
        </section>
      </main>
    </div>
  )
}

function ShellLoading() {
  return <div className="grid min-h-screen place-items-center text-sm text-zinc-500">加载中</div>
}

function Nav(props: { id: string; tab: string; setTab: (v: string) => void; icon: React.ReactNode; text: string }) {
  return (
    <button
      className={`flex h-9 items-center gap-2 rounded-md px-3 text-sm ${props.tab === props.id ? 'bg-zinc-900 text-white' : 'text-zinc-600 hover:bg-zinc-100'}`}
      onClick={() => props.setTab(props.id)}
    >
      {props.icon}
      {props.text}
    </button>
  )
}

function SetupPage({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const mutation = useMutation({
    mutationFn: () => api('/api/setup', { method: 'POST', body: JSON.stringify({ username, password }) }),
    onSuccess: onDone,
  })
  return (
    <AuthFrame title="初始化管理员">
      <FormError error={mutation.error} />
      <div className="field">
        <label>用户名</label>
        <Input value={username} onChange={(e) => setUsername(e.target.value)} />
      </div>
      <div className="field">
        <label>密码</label>
        <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
      </div>
      <Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>创建管理员</Button>
    </AuthFrame>
  )
}

function LoginPage({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const mutation = useMutation({
    mutationFn: () => api('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
    onSuccess: onDone,
  })
  return (
    <AuthFrame title="登录">
      <FormError error={mutation.error} />
      <div className="field">
        <label>用户名</label>
        <Input value={username} onChange={(e) => setUsername(e.target.value)} />
      </div>
      <div className="field">
        <label>密码</label>
        <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
      </div>
      <Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>登录</Button>
    </AuthFrame>
  )
}

function AuthFrame({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="grid min-h-screen place-items-center bg-[#f6f7f9] p-4">
      <div className="grid w-full max-w-sm gap-4 rounded-lg border border-zinc-200 bg-white p-5 shadow-sm">
        <h1 className="text-xl font-semibold">{title}</h1>
        {children}
      </div>
    </div>
  )
}

function FormError({ error }: { error: unknown }) {
  if (!error) return null
  return <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{(error as Error).message}</div>
}

function StatusPage() {
  const [windowValue, setWindowValue] = useState('1h')
  const q = useQuery({ queryKey: ['status', windowValue], queryFn: () => api<MonitorStatus>(`/api/monitor/status?window=${windowValue}`), refetchInterval: 60000 })
  const cards = q.data?.rows ?? []
  return (
    <Page title="状态监控" actions={<WindowSelect value={windowValue} setValue={setWindowValue} />}>
      <div className="grid gap-3 sm:grid-cols-4">
        <Metric label="请求数" value={q.data?.requests ?? 0} />
        <Metric label="成功" value={q.data?.success ?? 0} />
        <Metric label="失败" value={q.data?.failed ?? 0} />
        <Metric label="平均延迟" value={`${q.data?.avg_latency ?? 0} ms`} />
      </div>
      <div className="table-wrap">
        <table>
          <thead>
            <tr><th>卡片</th><th>上游</th><th>Key</th><th>状态点</th><th>错误</th><th>操作</th></tr>
          </thead>
          <tbody>
            {cards.map((c) => <CardRow key={c.id} card={c} />)}
            {cards.length === 0 && <EmptyRow colSpan={6} text="暂无卡片" />}
          </tbody>
        </table>
      </div>
    </Page>
  )
}

function CardRow({ card }: { card: Card }) {
  const qc = useQueryClient()
  const check = useMutation({
    mutationFn: () => api(`/api/cards/${card.id}/check`, { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['status'] }),
  })
  return (
    <tr>
      <td className="font-medium">{card.name}</td>
      <td>{card.upstream_name}</td>
      <td>{card.key_name || card.key_group || '-'}</td>
      <td>
        <div className="flex gap-1">
          {(card.history ?? []).slice(-40).map((p, i) => (
            <span
              key={`${p.checked_at}-${i}`}
              className={`dot ${p.success ? 'bg-emerald-500' : 'bg-red-500'}`}
              title={`${p.success ? '成功' : '失败'} / ${p.latency_ms}ms / ${fmtTime(p.checked_at)}${p.error ? ` / ${p.error}` : ''}`}
            />
          ))}
        </div>
      </td>
      <td className="max-w-[260px] truncate text-red-600">{card.last_error || '-'}</td>
      <td><Button variant="outline" size="sm" onClick={() => check.mutate()}><RefreshCcw className="size-3" />检查</Button></td>
    </tr>
  )
}

function BalancesPage() {
  const q = useQuery({ queryKey: ['balances'], queryFn: () => api<BalanceRow[]>('/api/monitor/balances'), refetchInterval: 60000 })
  return (
    <Page title="余额监控">
      <div className="table-wrap">
        <table>
          <thead><tr><th>上游</th><th>类型</th><th>倍率</th><th>余额 RMB</th><th>源余额</th><th>更新时间</th><th>状态</th></tr></thead>
          <tbody>
            {(q.data ?? []).map((b) => (
              <tr key={b.id}>
                <td className="font-medium">{b.name}</td>
                <td>{b.type}</td>
                <td>{b.balance_rate}</td>
                <td>{num(b.remain)}</td>
                <td>{num(b.source_remain)}</td>
                <td>{fmtTime(b.last_check)}</td>
                <td>{b.low_balance ? <span className="text-red-600">低余额</span> : <span className="text-emerald-700">正常</span>}</td>
              </tr>
            ))}
            {(q.data ?? []).length === 0 && <EmptyRow colSpan={7} text="暂无余额数据" />}
          </tbody>
        </table>
      </div>
    </Page>
  )
}

function UpstreamsPage() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamRow[]>('/api/upstreams') })
  const cards = useQuery({ queryKey: ['cards'], queryFn: () => api<Card[]>('/api/cards') })
  const createCard = useMutation({
    mutationFn: (body: { upstream_id: string; key_id: string }) => api('/api/cards', { method: 'POST', body: JSON.stringify(body) }),
    onSuccess: () => void Promise.all([qc.invalidateQueries({ queryKey: ['cards'] }), qc.invalidateQueries({ queryKey: ['status'] })]),
  })
  return (
    <Page title="上游管理" actions={<UpstreamDialog />}>
      <div className="table-wrap">
        <table>
          <thead><tr><th>名称</th><th>类型</th><th>地址</th><th>Key</th><th>余额</th><th>错误</th><th>操作</th></tr></thead>
          <tbody>
            {(q.data ?? []).map((row) => (
              <tr key={row.upstream.id}>
                <td className="font-medium">{row.upstream.name}</td>
                <td>{row.upstream.type}</td>
                <td className="max-w-[260px] truncate">{row.upstream.base_url}</td>
                <td>{row.keys.length}</td>
                <td>{num(row.balance?.remain)}</td>
                <td className="max-w-[220px] truncate text-red-600">{row.upstream.last_error || row.balance?.error || '-'}</td>
                <td className="flex gap-2">
                  <UpstreamDialog upstream={row.upstream} />
                  <Action path={`/api/upstreams/${row.upstream.id}/sync-keys`} label="同步 Key" />
                  <Action path={`/api/upstreams/${row.upstream.id}/check`} label="检查" />
                </td>
              </tr>
            ))}
            {(q.data ?? []).length === 0 && <EmptyRow colSpan={7} text="暂无上游" />}
          </tbody>
        </table>
      </div>
      <section className="grid gap-3">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-semibold">状态卡片</h2>
          <CardDialog rows={q.data ?? []} onSubmit={(body) => createCard.mutate(body)} />
        </div>
        <div className="table-wrap">
          <table>
            <thead><tr><th>名称</th><th>上游</th><th>Key</th><th>启用</th></tr></thead>
            <tbody>
              {(cards.data ?? []).map((c) => <tr key={c.id}><td>{c.name}</td><td>{c.upstream_name}</td><td>{c.key_name || c.key_group || '-'}</td><td>{c.enabled ? '是' : '否'}</td></tr>)}
              {(cards.data ?? []).length === 0 && <EmptyRow colSpan={4} text="暂无卡片" />}
            </tbody>
          </table>
        </div>
      </section>
    </Page>
  )
}

function UpstreamDialog({ upstream }: { upstream?: Upstream }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<Upstream>(upstream ?? emptyUpstream)
  const save = useMutation({
    mutationFn: () => api(upstream ? `/api/upstreams/${upstream.id}` : '/api/upstreams', { method: upstream ? 'PATCH' : 'POST', body: JSON.stringify(form) }),
    onSuccess: () => {
      setOpen(false)
      void qc.invalidateQueries({ queryKey: ['upstreams'] })
    },
  })
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant={upstream ? 'outline' : 'default'} size="sm">{upstream ? '编辑' : <><Plus className="size-4" />新增上游</>}</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>{upstream ? '编辑上游' : '新增上游'}</DialogTitle>
        <FormError error={save.error} />
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="名称"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
          <Field label="类型">
            <Select value={form.type} onValueChange={(v) => setForm({ ...form, type: v as Upstream['type'] })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value="newapi">new-api</SelectItem><SelectItem value="sub2api">sub2api</SelectItem></SelectContent>
            </Select>
          </Field>
          <Field label="Base URL"><Input value={form.base_url} onChange={(e) => setForm({ ...form, base_url: e.target.value })} /></Field>
          <Field label="余额倍率"><Input type="number" value={form.balance_rate} onChange={(e) => setForm({ ...form, balance_rate: Number(e.target.value) })} /></Field>
          <Field label="低余额阈值"><Input type="number" value={form.low_balance_threshold} onChange={(e) => setForm({ ...form, low_balance_threshold: Number(e.target.value) })} /></Field>
          {form.type === 'newapi' ? (
            <>
              <Field label="New-Api-User"><Input value={form.user_id ?? ''} onChange={(e) => setForm({ ...form, user_id: e.target.value })} /></Field>
              <Field label="Access Token"><Input value={form.access_token ?? ''} onChange={(e) => setForm({ ...form, access_token: e.target.value })} /></Field>
            </>
          ) : (
            <>
              <Field label="邮箱"><Input value={form.email ?? ''} onChange={(e) => setForm({ ...form, email: e.target.value })} /></Field>
              <Field label="密码"><Input type="password" value={form.password ?? ''} onChange={(e) => setForm({ ...form, password: e.target.value })} /></Field>
              <Field label="Access Token"><Input value={form.sub2api_access_token ?? ''} onChange={(e) => setForm({ ...form, sub2api_access_token: e.target.value })} /></Field>
              <Field label="Refresh Token"><Input value={form.sub2api_refresh_token ?? ''} onChange={(e) => setForm({ ...form, sub2api_refresh_token: e.target.value })} /></Field>
            </>
          )}
        </div>
        <div className="flex justify-end"><Button onClick={() => save.mutate()}><Save className="size-4" />保存</Button></div>
      </DialogContent>
    </Dialog>
  )
}

function CardDialog({ rows, onSubmit }: { rows: UpstreamRow[]; onSubmit: (body: { upstream_id: string; key_id: string }) => void }) {
  const [open, setOpen] = useState(false)
  const [upstreamID, setUpstreamID] = useState('')
  const [keyID, setKeyID] = useState('')
  const keys = useMemo(() => rows.find((r) => r.upstream.id === upstreamID)?.keys ?? [], [rows, upstreamID])
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild><Button size="sm"><KeyRound className="size-4" />新增卡片</Button></DialogTrigger>
      <DialogContent>
        <DialogTitle>新增状态卡片</DialogTitle>
        <Field label="上游">
          <Select value={upstreamID} onValueChange={(v) => { setUpstreamID(v); setKeyID('') }}>
            <SelectTrigger><SelectValue placeholder="选择上游" /></SelectTrigger>
            <SelectContent>{rows.map((r) => <SelectItem key={r.upstream.id} value={r.upstream.id}>{r.upstream.name}</SelectItem>)}</SelectContent>
          </Select>
        </Field>
        <Field label="Key">
          <Select value={keyID} onValueChange={setKeyID}>
            <SelectTrigger><SelectValue placeholder="选择 Key" /></SelectTrigger>
            <SelectContent>{keys.map((k) => <SelectItem key={k.id} value={k.id}>{k.name || k.description || k.id}</SelectItem>)}</SelectContent>
          </Select>
        </Field>
        <div className="flex justify-end"><Button onClick={() => { onSubmit({ upstream_id: upstreamID, key_id: keyID }); setOpen(false) }}>保存</Button></div>
      </DialogContent>
    </Dialog>
  )
}

function SettingsPage() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['settings'], queryFn: () => api<SettingsData>('/api/settings') })
  const [form, setForm] = useState<SettingsData | null>(null)
  const data = form ?? q.data
  const save = useMutation({
    mutationFn: () => api('/api/settings', { method: 'PATCH', body: JSON.stringify(data) }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['settings'] }),
  })
  if (!data) return <ShellLoading />
  return (
    <Page title="设置">
      <div className="grid max-w-xl gap-4 rounded-lg border border-zinc-200 bg-white p-4">
        <Field label="检查间隔（分钟）"><Input type="number" value={data.check_interval_minutes} onChange={(e) => setForm({ ...data, check_interval_minutes: Number(e.target.value) })} /></Field>
        <Field label="Telegram Bot Token"><Input value={data.telegram_bot_token ?? ''} onChange={(e) => setForm({ ...data, telegram_bot_token: e.target.value })} /></Field>
        <Field label="Telegram Chat ID"><Input value={data.telegram_chat_id ?? ''} onChange={(e) => setForm({ ...data, telegram_chat_id: e.target.value })} /></Field>
        <Field label="探测模型"><Input value={data.probe_model} disabled /></Field>
        <div><Button onClick={() => save.mutate()}><Save className="size-4" />保存</Button></div>
      </div>
    </Page>
  )
}

function Action({ path, label }: { path: string; label: string }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => api(path, { method: 'POST' }),
    onSuccess: () => void Promise.all([qc.invalidateQueries({ queryKey: ['upstreams'] }), qc.invalidateQueries({ queryKey: ['balances'] }), qc.invalidateQueries({ queryKey: ['status'] })]),
  })
  return <Button variant="outline" size="sm" onClick={() => mutation.mutate()} disabled={mutation.isPending}>{label}</Button>
}

function WindowSelect({ value, setValue }: { value: string; setValue: (v: string) => void }) {
  return (
    <Select value={value} onValueChange={setValue}>
      <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
      <SelectContent>{['1h', '3h', '5h', '1d', '7d', '15d'].map((v) => <SelectItem key={v} value={v}>{v}</SelectItem>)}</SelectContent>
    </Select>
  )
}

function Page({ title, actions, children }: { title: string; actions?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="grid gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold tracking-normal">{title}</h1>
        {actions}
      </div>
      {children}
    </div>
  )
}

function Metric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-zinc-200 bg-white p-4">
      <div className="text-sm text-zinc-500">{label}</div>
      <div className="mt-1 text-2xl font-semibold">{value}</div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="field"><label>{label}</label>{children}</div>
}

function EmptyRow({ colSpan, text }: { colSpan: number; text: string }) {
  return <tr><td colSpan={colSpan} className="py-10 text-center text-zinc-500">{text}</td></tr>
}

function num(v: number | undefined) {
  if (v === undefined || Number.isNaN(v)) return '-'
  return Number(v).toFixed(2)
}

function fmtTime(v?: string) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return '-'
  return d.toLocaleString('zh-CN', { hour12: false })
}
