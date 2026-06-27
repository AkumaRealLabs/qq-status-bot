import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, ExternalLink, KeyRound, Loader2, Plus, RefreshCcw, Save, Trash2, WalletCards } from 'lucide-react'
import { EmptyPanel, Field, FormError, HoverText, IconAction, MiniStat, SkeletonCardGrid, StatusBadge, TypeBadge } from '@/components/common'
import { Page } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage, fmtTime, num } from '@/lib/format'
import { useAutoClear } from '@/lib/hooks'
import { invalidateMonitor } from '@/lib/query'
import { cn } from '@/lib/utils'
import type { RechargeCapabilities, RechargeLog, RechargeResult, Upstream, UpstreamRow } from '@/types'

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

export function UpstreamsPage() {
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamRow[]>('/api/upstreams') })
  const upstreamRows = upstreams.data ?? []
  return (
    <Page
      title="上游管理"
      description="上游凭据、Key 和连接状态"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {upstreams.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <UpstreamDialog />
        </div>
      }
    >
      {upstreams.isLoading && <SkeletonCardGrid count={6} />}
      {!upstreams.isLoading && upstreamRows.length > 0 && (
        <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {upstreamRows.map((row) => <UpstreamCard key={row.upstream.id} row={row} />)}
        </div>
      )}
      {!upstreams.isLoading && upstreamRows.length === 0 && <EmptyPanel text="暂无上游" />}
    </Page>
  )
}

function UpstreamCard({ row }: { row: UpstreamRow }) {
  const upstream = row.upstream
  const error = upstream.last_error || row.balance?.error || ''
  return (
    <Card className={cn('min-w-0 bg-card', error && 'border-destructive/40')}>
      <CardHeader className="gap-2 border-b border-border">
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-3">
          <div className="min-w-0">
            <CardTitle className="break-words">{upstream.name}</CardTitle>
            <CardDescription className="mt-1">
              <div className="flex flex-wrap items-center gap-2">
                <TypeBadge type={upstream.type} />
              </div>
            </CardDescription>
          </div>
          <StatusBadge ok={upstream.enabled} okText="启用" failText="停用" />
        </div>
      </CardHeader>
      <CardContent className="grid min-w-0 gap-3 pt-1">
        <div className="grid min-w-0 gap-2">
          <MiniStat label="Key 数量" value={keysOf(row).length} />
        </div>
        <div className="grid min-w-0 gap-1">
          <div className="text-xs text-muted-foreground">Base URL</div>
          <HoverText value={upstream.base_url} className="rounded-sm border border-border bg-background px-2.5 py-1.5 text-xs text-muted-foreground" alwaysTooltip />
        </div>
        <div className="grid min-w-0 gap-1">
          <div className="text-xs text-muted-foreground">错误信息</div>
          <HoverText
            value={error || '-'}
            className={cn('rounded-sm px-2.5 py-1.5 text-xs', error ? 'bg-destructive/10 text-destructive' : 'border border-border bg-background text-muted-foreground')}
            alwaysTooltip={Boolean(error)}
          />
        </div>
        <UpstreamActions row={row} />
      </CardContent>
    </Card>
  )
}

function UpstreamActions({ row }: { row: UpstreamRow }) {
  const qc = useQueryClient()
  const upstream = row.upstream
  const remove = useMutation({
    mutationFn: () => api(`/api/upstreams/${upstream.id}`, { method: 'DELETE' }),
    onSuccess: () => void invalidateMonitor(qc),
    onError: (error) => window.alert(errorMessage(error)),
  })
  return (
    <div className="grid max-w-full grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
      <div className="flex min-w-0 flex-wrap gap-2">
        <UpstreamDialog upstream={upstream} />
        <Action path={`/api/upstreams/${upstream.id}/check`} label="刷新数据" />
      </div>
      <IconAction title="删除" onClick={() => confirmDelete(upstream.name) && remove.mutate()} pending={remove.isPending} icon={Trash2} danger />
    </div>
  )
}

export function BalanceRechargeDialog({ upstream }: { upstream: Upstream }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [amount, setAmount] = useState('')
  const [paymentType, setPaymentType] = useState('')
  const [code, setCode] = useState('')
  const [result, setResult] = useState<RechargeResult | null>(null)
  const [pollOrderID, setPollOrderID] = useState('')
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
  const methods = useMemo(() => caps.data?.methods ?? [], [caps.data?.methods])
  const selectedMethod = methods.find((item) => item.type === paymentType) ?? methods[0]
  const amountNumber = Number(amount)
  const amountRequired = !paymentType.startsWith('creem:')
  const minAmount = selectedMethod?.min_amount || 0
  const maxAmount = selectedMethod?.max_amount || 0
  const amountError = amountRequired
    ? !amount.trim() || !Number.isFinite(amountNumber) || amountNumber <= 0
      ? '请输入充值金额'
      : minAmount > 0 && amountNumber < minAmount
      ? `最低充值 ${num(minAmount)}`
      : maxAmount > 0 && amountNumber > maxAmount
        ? `最高充值 ${num(maxAmount)}`
        : ''
    : ''
  useEffect(() => {
    if (open && methods[0] && !methods.some((method) => method.type === paymentType)) setPaymentType(methods[0].type)
  }, [methods, open, paymentType])
  useEffect(() => {
    if (!open || !pollOrderID) return
    const log = (logs.data ?? []).find((item) => item.remote_order_id === pollOrderID)
    if (!log || rechargePollDone(log)) return
    const timer = window.setTimeout(() => {
      void api<RechargeLog>(`/api/upstreams/${upstream.id}/balance-recharge/logs/${log.id}/refresh`, { method: 'POST' })
        .catch(() => undefined)
        .then(() => Promise.all([
          invalidateMonitor(qc),
          qc.invalidateQueries({ queryKey: ['balance-recharge-logs', upstream.id] }),
        ]))
    }, 3000)
    return () => window.clearTimeout(timer)
  }, [logs.data, open, pollOrderID, qc, upstream.id])
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
      setPollOrderID(out.remote_order_id ?? '')
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
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) setResult(null)
        if (!next) setPollOrderID('')
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
          <div className="grid min-w-0 items-start gap-4 rounded-sm border border-border bg-card p-3 md:grid-cols-2">
            <Field label="金额">
              <div className="grid gap-1.5">
                <Input type="number" min={minAmount || 0} max={maxAmount || undefined} value={amount} onChange={(e) => setAmount(e.target.value)} />
                <div className={cn('min-h-4 text-xs', amountError ? 'text-destructive' : 'text-muted-foreground')}>
                  {amountError || amountHint(minAmount, maxAmount) || '\u00a0'}
                </div>
              </div>
            </Field>
            <Field label="支付方式">
              <div className="grid gap-1.5">
                <Select value={paymentType} onValueChange={setPaymentType}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {methods.map((method) => (
                      <SelectItem key={method.type} value={method.type}>{method.name || method.type}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <div className="min-h-4 text-xs text-muted-foreground">&nbsp;</div>
              </div>
            </Field>
            <div className="md:col-span-2">
              <Button onClick={() => createOrder.mutate()} disabled={busy || Boolean(amountError) || !paymentType}>
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
        <RechargeLogs upstreamID={upstream.id} logs={logs.data ?? []} loading={logs.isLoading} />
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

function RechargeLogs({ upstreamID, logs, loading }: { upstreamID: string; logs: RechargeLog[]; loading: boolean }) {
  const qc = useQueryClient()
  const [refreshingID, setRefreshingID] = useState('')
  const [removingID, setRemovingID] = useState('')
  const refresh = useMutation({
    mutationFn: (id: string) => api<RechargeLog>(`/api/upstreams/${upstreamID}/balance-recharge/logs/${id}/refresh`, { method: 'POST' }),
    onMutate: (id) => setRefreshingID(id),
    onSuccess: async () => {
      await Promise.all([
        invalidateMonitor(qc),
        qc.invalidateQueries({ queryKey: ['balance-recharge-logs', upstreamID] }),
      ])
    },
    onError: (error) => window.alert(errorMessage(error)),
    onSettled: () => setRefreshingID(''),
  })
  const remove = useMutation({
    mutationFn: (id: string) => api(`/api/upstreams/${upstreamID}/balance-recharge/logs/${id}`, { method: 'DELETE' }),
    onMutate: (id) => setRemovingID(id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['balance-recharge-logs', upstreamID] })
    },
    onError: (error) => window.alert(errorMessage(error)),
    onSettled: () => setRemovingID(''),
  })
  if (loading) return <div className="text-sm text-muted-foreground">加载记录...</div>
  if (logs.length === 0) return null
  return (
    <div className="grid min-w-0 gap-2">
      <div className="text-sm font-medium text-foreground">操作记录</div>
      <div className="grid max-h-52 min-w-0 gap-2 overflow-auto">
        {logs.map((log) => (
          <RechargeLogItem
            key={log.id}
            log={log}
            refreshing={refreshingID === log.id}
            removing={removingID === log.id}
            busy={refresh.isPending || remove.isPending}
            onRefresh={() => refresh.mutate(log.id)}
            onDelete={() => confirmDelete('这条操作记录') && remove.mutate(log.id)}
          />
        ))}
      </div>
    </div>
  )
}

function RechargeLogItem({
  log,
  refreshing,
  removing,
  busy,
  onRefresh,
  onDelete,
}: {
  log: RechargeLog
  refreshing: boolean
  removing: boolean
  busy: boolean
  onRefresh: () => void
  onDelete: () => void
}) {
  return (
    <div className="grid min-w-0 gap-1 rounded-sm border border-border bg-background p-2 text-xs">
      <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
        <span className="min-w-0 truncate">{log.method === 'redeem' ? '兑换码' : paymentLabel(log.payment_type)}</span>
        <div className="flex min-w-0 items-center justify-end gap-1">
          <span className={rechargeStatusClass(log.status, log.raw_status)}>{rechargeStatusLabel(log.status, log.raw_status)}</span>
          {log.method === 'order' && log.remote_order_id && (
            <Button variant="ghost" size="icon" className="size-6" onClick={onRefresh} disabled={busy}>
              {refreshing ? <Loader2 className="size-3 animate-spin" /> : <RefreshCcw className="size-3" />}
              <span className="sr-only">刷新状态</span>
            </Button>
          )}
          <Button variant="ghost" size="icon" className="size-6 text-destructive hover:text-destructive" onClick={onDelete} disabled={busy}>
            {removing ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />}
            <span className="sr-only">删除记录</span>
          </Button>
        </div>
      </div>
      <div className="text-muted-foreground">{fmtTime(log.created_at)} · {num(log.amount)}</div>
      {log.remote_order_id && <div className="break-all text-muted-foreground">订单：{log.remote_order_id}</div>}
      {rechargeDisplayMessage(log.message) && (
        <HoverText value={rechargeRawDetail(log)} className="text-muted-foreground" alwaysTooltip>
          <span className="block break-words">{rechargeDisplayMessage(log.message)}</span>
        </HoverText>
      )}
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

function Action({ path, label }: { path: string; label: string }) {
  const qc = useQueryClient()
  const [message, setMessage] = useState('')
  useAutoClear(message, `${label}完成`, setMessage)
  const mutation = useMutation({
    mutationFn: () => api(path, { method: 'POST' }),
    onMutate: () => setMessage(`${label}中...`),
    onSuccess: async () => {
      setMessage(`${label}完成`)
      await invalidateMonitor(qc)
    },
    onError: (error) => setMessage(errorMessage(error)),
  })
  return (
    <div className="relative min-w-0">
      <Button variant="outline" size="sm" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
        {mutation.isPending && <Loader2 className="size-3 animate-spin" />}
        {mutation.isPending ? `${label}中` : label}
      </Button>
      {message && !mutation.isPending && <div className="absolute right-0 top-10 z-10 max-w-[calc(100vw-32px)] whitespace-normal break-words rounded-sm border border-border bg-background px-2 py-1 text-xs text-muted-foreground">{message}</div>}
    </div>
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

function closeBrowserLoginWindow() {
  browserLoginWindow?.close()
  browserLoginWindow = null
  const win = window.open('', 'ai-upstream-monitor-vnc')
  win?.close()
}

function keysOf(row: UpstreamRow | undefined) {
  return row?.keys ?? []
}

function confirmDelete(name: string) {
  return window.confirm(`确认删除 ${name}？`)
}

function paymentLabel(value: string) {
  return ({
    alipay: '支付宝',
    wxpay: '微信支付',
    stripe: 'Stripe',
    airwallex: 'Airwallex',
    epay: '在线支付',
    muyin: 'Muyin',
    waffo: 'Waffo',
    'waffo-pancake': 'Waffo Pancake',
  } as Record<string, string>)[value] || value || '-'
}

function amountHint(minAmount: number, maxAmount: number) {
  return [minAmount > 0 ? `最低 ${num(minAmount)}` : '', maxAmount > 0 ? `最高 ${num(maxAmount)}` : ''].filter(Boolean).join(' · ')
}

function rechargeStatusLabel(status: string, rawStatus?: string) {
  const value = rechargeStatusKey(rawStatus || status)
  return ({
    success: '成功',
    completed: '成功',
    paid: '已支付',
    pending: '待支付/处理中',
    recharging: '充值中',
    processing: '处理中',
    failed: '失败',
    expired: '已过期',
    cancelled: '已取消',
    canceled: '已取消',
    refund_failed: '退款失败',
  } as Record<string, string>)[value] || rawStatus || status || '-'
}

function rechargeStatusClass(status: string, rawStatus?: string) {
  const value = rechargeStatusKey(rawStatus || status)
  return cn(
    ['success', 'paid', 'completed'].includes(value) && 'text-success',
    ['failed', 'expired', 'cancelled', 'canceled', 'refund_failed'].includes(value) && 'text-destructive',
    !rechargeStatusDone(status, rawStatus) && 'text-muted-foreground',
  )
}

function rechargeStatusKey(status?: string) {
  return (status || '').trim().toLowerCase()
}

function rechargeStatusDone(status: string, rawStatus?: string) {
  return ['success', 'paid', 'completed', 'failed', 'expired', 'cancelled', 'canceled', 'refund_failed'].includes(rechargeStatusKey(rawStatus || status))
}

function rechargePollDone(log: RechargeLog) {
  return rechargeStatusDone(log.status, log.raw_status) || /order not found/i.test(log.message)
}

function rechargeDisplayMessage(message: string) {
  const text = message.trim()
  if (!text || rechargeStatusLabel(text) !== text) return ''
  return errorMessage(text)
}

function rechargeRawDetail(log: RechargeLog) {
  return [log.raw_status ? `原始状态：${log.raw_status}` : '', log.message ? `原始信息：${log.message}` : ''].filter(Boolean).join('\n')
}
