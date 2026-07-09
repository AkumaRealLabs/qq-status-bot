import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, Loader2, RefreshCcw, RotateCcw, Settings2, Trash2, Upload } from 'lucide-react'
import { ActionRow, DataTable, EmptyPanel, Field, FormError, FeedbackBanner, IconAction, Metric, SaveButton } from '@/components/common'
import { Page, ShellLoading } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input, Textarea } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api, ApiError } from '@/lib/api'
import { errorMessage, fmtShortTime, fmtTime } from '@/lib/format'
import { alertError, closeAfterSave, confirmDelete, secretPlaceholder, useFeedback } from '@/lib/feedback'
import { cn } from '@/lib/utils'
import type { CLIProxyAccount, CLIProxyConfig, CLIProxyQuota, CLIProxyQuotaWindow } from '@/types'

const emptyConfig: CLIProxyConfig = { name: 'CLIProxyAPI', base_url: '', management_key: '', management_key_set: false, enabled: true }

export function CLIProxyPoolPage() {
  const qc = useQueryClient()
  const [quotaRefreshToken, setQuotaRefreshToken] = useState(0)
  const cfg = useQuery({ queryKey: ['cliproxy', 'config'], queryFn: () => api<CLIProxyConfig>('/api/pools/cliproxy/config') })
  const configured = Boolean(cfg.data?.enabled && cfg.data.base_url && cfg.data.management_key_set)
  const accounts = useQuery({
    queryKey: ['cliproxy', 'accounts'],
    queryFn: () => api<CLIProxyAccount[]>('/api/pools/cliproxy/accounts'),
    enabled: configured,
  })
  const rows = useMemo(() => sortAccounts(accounts.data ?? []), [accounts.data])
  const stats = useMemo(() => accountStats(rows), [rows])
  const refreshing = cfg.isFetching || accounts.isFetching
  const refreshAll = async () => {
    void cfg.refetch()
    if (configured) {
      await accounts.refetch()
      setQuotaRefreshToken((value) => value + 1)
    }
  }
  const remove = useMutation({
    mutationFn: (name: string) => api(`/api/pools/cliproxy/accounts/${encodeURIComponent(name)}`, { method: 'DELETE' }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['cliproxy', 'accounts'] })
    },
    onError: alertError,
  })
  const reset = useMutation({
    mutationFn: (name: string) => api(`/api/pools/cliproxy/accounts/${encodeURIComponent(name)}/reset-quota`, { method: 'POST', body: JSON.stringify({}) }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['cliproxy', 'accounts'] })
    },
    onError: alertError,
  })

  if (cfg.isLoading) return <ShellLoading />
  const status = connectionStatus(cfg.data, accounts.error, configured)
  return (
    <Page
      title="号池管理"
      description="CLIProxyAPI 授权账号文件"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {refreshing && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <Button variant="outline" size="sm" onClick={() => void refreshAll()} disabled={refreshing}>
            <RefreshCcw className={cn('size-4', refreshing && 'animate-spin')} />
            刷新
          </Button>
          <UploadDialog disabled={!configured} />
          <ConfigDialog config={cfg.data ?? emptyConfig} />
        </div>
      }
    >
      <div className="grid min-w-0 gap-3 md:grid-cols-4">
        <Metric label="连接状态" value={status.text} accent={status.ok ? 'success' : status.danger ? 'danger' : undefined} />
        <Metric label="账号总数" value={configured ? rows.length : '-'} />
        <Metric label="不可用" value={configured ? stats.bad : '-'} accent={stats.bad ? 'danger' : undefined} />
        <Metric label="近期失败" value={configured ? stats.failed : '-'} accent={stats.failed ? 'danger' : undefined} />
      </div>

      <FormError error={cfg.error || accounts.error} />
      {!configured && <EmptyPanel text="请先配置 CLIProxyAPI 管理地址和管理密钥" />}
      {configured && accounts.isLoading && <EmptyPanel text="加载中..." />}
      {configured && !accounts.isLoading && !accounts.isError && rows.length === 0 && <EmptyPanel text="暂无授权账号文件" />}
      {configured && rows.length > 0 && (
        <Card className="min-w-0 bg-card">
          <CardContent>
            <DataTable
              minWidthClass="min-w-[1360px]"
              head={
                <tr>
                  <th className="w-64 whitespace-nowrap px-3 py-2 font-medium">文件</th>
                  <th className="w-28 whitespace-nowrap px-3 py-2 font-medium">状态</th>
                  <th className="w-64 whitespace-nowrap px-3 py-2 font-medium">账号</th>
                  <th className="w-32 whitespace-nowrap px-3 py-2 font-medium">类型</th>
                  <th className="w-72 whitespace-nowrap px-3 py-2 font-medium">额度</th>
                  <th className="w-24 whitespace-nowrap px-3 py-2 font-medium">成功/失败</th>
                  <th className="w-40 whitespace-nowrap px-3 py-2 font-medium">更新时间</th>
                  <th className="w-36 whitespace-nowrap px-3 py-2 text-right font-medium">操作</th>
                </tr>
              }
            >
              {rows.map((account) => (
                <tr key={account.name} className="border-t border-border align-top">
                  <td className="w-64 max-w-64 px-3 py-2">
                    <div className="truncate font-medium text-foreground">{account.name}</div>
                    <div className="truncate text-xs text-muted-foreground">{account.auth_index ? `#${account.auth_index}` : sizeText(account.size)}</div>
                  </td>
                  <td className="w-28 whitespace-nowrap px-3 py-2">
                    <AccountBadge account={account} />
                    {account.status_message && <div className="mt-1 max-w-56 truncate text-xs text-muted-foreground">{account.status_message}</div>}
                  </td>
                  <td className="w-64 max-w-64 px-3 py-2">
                    <div className="truncate">{account.email || account.account || '-'}</div>
                    {account.source && <div className="truncate text-xs text-muted-foreground">{account.source}</div>}
                  </td>
                  <td className="w-32 whitespace-nowrap px-3 py-2">
                    <div className="truncate">{account.provider || account.type || '-'}</div>
                    <div className="truncate text-xs text-muted-foreground">{account.account_type || '-'}</div>
                  </td>
                  <td className="w-72 px-3 py-2">
                    <AccountQuota account={account} refreshToken={quotaRefreshToken} />
                  </td>
                  <td className="w-24 whitespace-nowrap px-3 py-2">
                    <span className="text-success">{account.success ?? 0}</span>
                    <span className="text-muted-foreground"> / </span>
                    <span className={cn((account.failed ?? 0) > 0 && 'text-destructive')}>{account.failed ?? 0}</span>
                  </td>
                  <td className="w-40 whitespace-nowrap px-3 py-2 text-muted-foreground">{fmtTime(account.updated_at || account.modtime || account.last_refresh)}</td>
                  <td className="w-36 whitespace-nowrap px-3 py-2">
                    <div className="flex justify-end gap-1.5">
                      <IconAction title="下载" icon={Download} pending={false} onClick={() => void downloadAccount(account.name).catch(alertError)} />
                      <IconAction
                        title="重置额度"
                        icon={RotateCcw}
                        pending={reset.isPending}
                        disabled={!account.auth_index}
                        onClick={() => account.auth_index && reset.mutate(account.name)}
                      />
                      <IconAction title="删除" icon={Trash2} pending={remove.isPending} danger onClick={() => confirmDelete(account.name) && remove.mutate(account.name)} />
                    </div>
                    {!account.auth_index && <div className="mt-1 text-right text-xs text-muted-foreground">无 auth_index</div>}
                  </td>
                </tr>
              ))}
            </DataTable>
          </CardContent>
        </Card>
      )}
    </Page>
  )
}

function ConfigDialog({ config }: { config: CLIProxyConfig }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<CLIProxyConfig>(config)
  const fb = useFeedback()
  const save = useMutation({
    mutationFn: () => api<CLIProxyConfig>('/api/pools/cliproxy/config', { method: 'PATCH', body: JSON.stringify(form) }),
    onMutate: () => fb.pending(),
    onSuccess: async (data) => {
      fb.success()
      await qc.setQueryData(['cliproxy', 'config'], data)
      await qc.invalidateQueries({ queryKey: ['cliproxy'] })
      closeAfterSave(setOpen, 450)
    },
    onError: fb.fail,
  })
  const update = (patch: Partial<CLIProxyConfig>) => {
    fb.clear()
    setForm((value) => ({ ...value, ...patch }))
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (next) {
          fb.clear()
          setForm({ ...emptyConfig, ...config, management_key: '' })
        }
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <Settings2 className="size-4" />
          配置
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>CLIProxyAPI 配置</DialogTitle>
        <div className="grid min-w-0 gap-4 md:grid-cols-2">
          <Field label="名称">
            <Input value={form.name} onChange={(event) => update({ name: event.target.value })} />
          </Field>
          <Field label="启用状态">
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
          <div className="md:col-span-2">
            <Field label="管理 API 地址">
              <Input value={form.base_url} placeholder="http://127.0.0.1:8317/v0/management" onChange={(event) => update({ base_url: event.target.value })} />
            </Field>
          </div>
          <div className="md:col-span-2">
            <Field label="管理密钥">
              <Input
                type="password"
                value={form.management_key ?? ''}
                placeholder={secretPlaceholder(config.management_key_set)}
                onChange={(event) => update({ management_key: event.target.value })}
              />
            </Field>
          </div>
        </div>
        <FeedbackBanner message={fb.message} error={save.isError} />
        <ActionRow>
          <SaveButton onClick={() => save.mutate()} pending={save.isPending} disabled={!form.name || !form.base_url} message={fb.message} />
        </ActionRow>
      </DialogContent>
    </Dialog>
  )
}

function UploadDialog({ disabled }: { disabled: boolean }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [files, setFiles] = useState<File[]>([])
  const [content, setContent] = useState('')
  const [dragging, setDragging] = useState(false)
  const upload = useMutation({
    mutationFn: async () => {
      if (files.length > 0) {
        for (const file of files) {
          await api('/api/pools/cliproxy/accounts', { method: 'POST', body: JSON.stringify({ name: file.name, content: await file.text() }) })
        }
        return
      }
      await api('/api/pools/cliproxy/accounts', { method: 'POST', body: JSON.stringify({ content }) })
    },
    onSuccess: async () => {
      setOpen(false)
      setFiles([])
      setContent('')
      await qc.invalidateQueries({ queryKey: ['cliproxy', 'accounts'] })
    },
  })
  const pickFiles = (list?: FileList | null) => {
    const next = Array.from(list ?? [])
    setFiles(next)
    if (next.length > 0) setContent('')
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm" disabled={disabled}>
          <Upload className="size-4" />
          上传
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>上传授权文件</DialogTitle>
        <FormError error={upload.error} />
        <div className="grid min-w-0 gap-4">
          <div
            className={cn(
              'rounded-md border border-dashed border-border bg-secondary/40 p-4 text-center transition-colors',
              dragging && 'border-primary bg-primary/5',
            )}
            onDragOver={(event) => {
              event.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(event) => {
              event.preventDefault()
              setDragging(false)
              pickFiles(event.dataTransfer.files)
            }}
          >
            <Upload className="mx-auto size-6 text-muted-foreground" />
            <div className="mt-2 text-sm font-medium">{files.length > 0 ? `已选择 ${files.length} 个文件` : '拖入 JSON 文件'}</div>
            {files.length > 0 && <div className="mt-1 truncate text-xs text-muted-foreground">{files.map((file) => file.name).join(', ')}</div>}
            <Input className="mt-3 bg-background" type="file" multiple accept=".json,application/json" onChange={(event) => pickFiles(event.target.files)} />
          </div>
          <Field label="粘贴 JSON">
            <Textarea
              value={content}
              className="min-h-56 font-mono text-sm"
              placeholder="文件上传和粘贴二选一"
              onChange={(event) => {
                setFiles([])
                setContent(event.target.value)
              }}
            />
          </Field>
        </div>
        <div className="flex justify-end">
          <Button onClick={() => upload.mutate()} disabled={upload.isPending || (files.length === 0 && !content.trim())}>
            {upload.isPending ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />}
            上传
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function AccountBadge({ account }: { account: CLIProxyAccount }) {
  const bad = accountBad(account)
  const disabled = account.disabled || /disabled|停用/i.test(account.status || '')
  const text = disabled ? '停用' : bad ? '异常' : account.status || '正常'
  return <Badge variant={disabled ? 'amber' : bad ? 'destructive' : 'success'}>{text}</Badge>
}

function AccountQuota({ account, refreshToken }: { account: CLIProxyAccount; refreshToken: number }) {
  const canLoad = Boolean(account.auth_index && isCodexAccount(account))
  const { data, error, isFetching, refetch } = useQuery({
    queryKey: ['cliproxy', 'quota', account.name],
    queryFn: () => api<CLIProxyQuota>(quotaURL(account)),
    enabled: false,
    retry: false,
  })
  useEffect(() => {
    if (canLoad && refreshToken > 0) void refetch()
  }, [canLoad, refetch, refreshToken])
  if (!canLoad) return <span className="text-muted-foreground">-</span>
  if (isFetching && !data) return <Loader2 className="size-4 animate-spin text-muted-foreground" />
  if (!data && error) return <span className="text-xs text-destructive" title={errorMessage(error)}>读取失败</span>
  if (!data) return <span className="text-xs text-muted-foreground">点刷新查看</span>

  const plan = planText(data?.plan_type || account.account_type)
  return (
    <div className="grid min-w-0 gap-1.5">
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
        {plan && (
          <span>
            套餐 <span className="font-medium text-foreground">{plan}</span>
          </span>
        )}
        <span>主动重置次数 {data?.rate_limit_reset_credits_available ?? '未记录'}</span>
        {isFetching && <Loader2 className="size-3 animate-spin" />}
      </div>
      {(data?.windows ?? []).slice(0, 2).map((window) => (
        <QuotaWindow key={window.id} window={window} />
      ))}
      {data && data.windows.length === 0 && <span className="text-xs text-muted-foreground">未返回额度窗口</span>}
    </div>
  )
}

function quotaURL(account: CLIProxyAccount) {
  const params = new URLSearchParams()
  if (account.auth_index) params.set('auth_index', account.auth_index)
  if (account.account) params.set('account', account.account)
  if (account.account_type) params.set('account_type', account.account_type)
  return `/api/pools/cliproxy/accounts/${encodeURIComponent(account.name)}/quota?${params}`
}

function QuotaWindow({ window }: { window: CLIProxyQuotaWindow }) {
  const percent = window.remaining_percent
  const safePercent = typeof percent === 'number' && Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) : undefined
  return (
    <div className="grid min-w-0 gap-1">
      <div className="flex min-w-0 items-center justify-between gap-2 text-xs">
        <span className="truncate text-muted-foreground">{window.label}</span>
        <span className="shrink-0 font-medium">{safePercent === undefined ? '--' : `${Math.round(safePercent)}%`}</span>
        {window.reset_at && <span className="shrink-0 text-muted-foreground">{fmtShortTime(window.reset_at)}</span>}
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-secondary">
        <div className={cn('h-full rounded-full', quotaBarColor(safePercent))} style={{ width: `${safePercent ?? 0}%` }} />
      </div>
    </div>
  )
}

function isCodexAccount(account: CLIProxyAccount) {
  return `${account.provider || ''} ${account.type || ''} ${account.name || ''}`.toLowerCase().includes('codex')
}

function planText(plan?: string) {
  const value = plan?.trim()
  if (!value) return ''
  const key = value.toLowerCase().replace(/[_-]/g, '')
  if (key === 'team') return 'Team'
  if (key === 'plus') return 'Plus'
  if (key === 'pro') return 'Pro'
  if (key === 'prolite') return 'Pro Lite'
  if (key === 'free') return 'Free'
  return value
}

function quotaBarColor(percent?: number) {
  if (percent === undefined) return 'bg-muted-foreground/40'
  if (percent < 30) return 'bg-destructive'
  if (percent < 70) return 'bg-warning'
  return 'bg-success'
}

function connectionStatus(config?: CLIProxyConfig, error?: unknown, configured?: boolean) {
  if (!config?.enabled) return { text: '停用', ok: false, danger: false }
  if (!config?.base_url || !config.management_key_set) return { text: '未配置', ok: false, danger: false }
  if (error) return { text: '异常', ok: false, danger: true }
  if (!configured) return { text: '未连接', ok: false, danger: false }
  return { text: '已连接', ok: true, danger: false }
}

function accountStats(rows: CLIProxyAccount[]) {
  return rows.reduce(
    (sum, account) => ({
      bad: sum.bad + (accountBad(account) ? 1 : 0),
      failed: sum.failed + (account.failed || 0),
    }),
    { bad: 0, failed: 0 },
  )
}

function sortAccounts(rows: CLIProxyAccount[]) {
  return [...rows].sort((a, b) => {
    const ar = accountRank(a)
    const br = accountRank(b)
    if (ar !== br) return ar - br
    const name = a.name.localeCompare(b.name, 'zh-CN')
    if (name !== 0) return name
    return new Date(b.updated_at || b.modtime || 0).getTime() - new Date(a.updated_at || a.modtime || 0).getTime()
  })
}

function accountRank(account: CLIProxyAccount) {
  if (accountBad(account)) return 0
  if (account.disabled) return 1
  return 2
}

function accountBad(account: CLIProxyAccount) {
  const text = `${account.status || ''} ${account.status_message || ''}`.toLowerCase()
  return Boolean(account.unavailable || text.includes('error') || text.includes('fail') || text.includes('expired') || text.includes('invalid'))
}

function sizeText(size?: number) {
  if (!size) return '-'
  if (size < 1024) return `${size} B`
  return `${(size / 1024).toFixed(1)} KB`
}

async function downloadAccount(name: string) {
  const res = await fetch(`/api/pools/cliproxy/accounts/${encodeURIComponent(name)}/download`, { credentials: 'include' })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new ApiError(data.error || `HTTP ${res.status}`, res.status)
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  URL.revokeObjectURL(url)
}

