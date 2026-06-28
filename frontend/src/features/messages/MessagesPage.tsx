import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, RefreshCcw, Save, Trash2 } from 'lucide-react'
import { EmptyPanel, Field, FormError, HoverText, IconAction, StatusBadge } from '@/components/common'
import { Page } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { fmtTime } from '@/lib/format'
import type { TGChannel, TGLoginForm, TGMessage, TGSessionStatus } from '@/types'

const emptyLogin: TGLoginForm = { api_id: '', api_hash: '', phone: '', code: '', password: '' }

export function MessagesPage() {
  const qc = useQueryClient()
  const [showChannels, setShowChannels] = useState(false)
  const status = useQuery({ queryKey: ['tg', 'session'], queryFn: () => api<TGSessionStatus>('/api/tg/session/status') })
  const channels = useQuery({ queryKey: ['tg', 'channels'], queryFn: () => api<TGChannel[]>('/api/tg/channels'), enabled: status.data?.authorized })
  const messages = useQuery({ queryKey: ['tg', 'messages'], queryFn: () => api<TGMessage[]>('/api/tg/messages?limit=100'), enabled: status.data?.authorized, refetchInterval: 60000 })
  const refresh = useMutation({
    mutationFn: () => api('/api/tg/messages/refresh', { method: 'POST', body: JSON.stringify({}) }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['tg'] })
    },
  })
  const authed = Boolean(status.data?.authorized)
  return (
    <Page
      title="最新消息"
      description="Telegram 用户会话抓取的频道消息"
      actions={
        authed && (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="outline" size="sm" onClick={() => setShowChannels((value) => !value)}>
              {showChannels ? '收起频道管理' : '频道管理'}
            </Button>
            <Button variant="outline" size="sm" onClick={() => refresh.mutate()} disabled={refresh.isPending}>
              {refresh.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCcw className="size-4" />}
              刷新消息
            </Button>
          </div>
        )
      }
    >
      <FormError error={status.error || refresh.error} />
      {(!authed || status.data?.last_error) && <SessionPanel status={status.data} loading={status.isLoading} />}
      {!authed ? <LoginWizard status={status.data} /> : (
        <>
          <MessageList messages={messages.data ?? []} channels={channels.data ?? []} loading={messages.isLoading} />
          {showChannels && <ChannelPanel channels={channels.data ?? []} loading={channels.isLoading} />}
        </>
      )}
    </Page>
  )
}

function SessionPanel({ status, loading }: { status?: TGSessionStatus; loading: boolean }) {
  const ok = Boolean(status?.authorized)
  return (
    <Card className="bg-card">
      <CardContent className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="text-sm text-muted-foreground">登录状态</div>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            {loading ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : <StatusBadge ok={ok} okText="已登录" failText="未登录" />}
            {status?.phone && <span className="text-sm text-muted-foreground">{status.phone}</span>}
            {status?.api_id && <span className="text-sm text-muted-foreground">API ID {status.api_id}</span>}
          </div>
        </div>
        {status?.last_error && <HoverText value={status.last_error} className="max-w-md rounded-sm bg-destructive/10 px-2.5 py-1.5 text-xs text-destructive" alwaysTooltip />}
      </CardContent>
    </Card>
  )
}

function LoginWizard({ status }: { status?: TGSessionStatus }) {
  const qc = useQueryClient()
  const [form, setForm] = useState(emptyLogin)
  const update = (patch: Partial<TGLoginForm>) => setForm((value) => ({ ...value, ...patch }))
  const start = useMutation({
    mutationFn: () => api<TGSessionStatus>('/api/tg/session/start', { method: 'POST', body: JSON.stringify({ api_id: Number(form.api_id), api_hash: form.api_hash, phone: form.phone }) }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['tg', 'session'] })
    },
  })
  const verify = useMutation({
    mutationFn: () => api<TGSessionStatus>('/api/tg/session/verify', { method: 'POST', body: JSON.stringify({ code: form.code }) }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['tg', 'session'] })
    },
  })
  const password = useMutation({
    mutationFn: () => api<TGSessionStatus>('/api/tg/session/password', { method: 'POST', body: JSON.stringify({ password: form.password }) }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['tg', 'session'] })
    },
  })
  return (
    <Card className="bg-card">
      <CardHeader>
        <CardTitle>登录 Telegram</CardTitle>
        <CardDescription>使用 Telegram API development tools 的 api_id 和 api_hash</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <FormError error={start.error || verify.error || password.error} />
        <div className="grid gap-4 md:grid-cols-3">
          <Field label="api_id">
            <Input inputMode="numeric" value={form.api_id} onChange={(e) => update({ api_id: e.target.value })} />
          </Field>
          <Field label="api_hash">
            <Input type="password" value={form.api_hash} onChange={(e) => update({ api_hash: e.target.value })} />
          </Field>
          <Field label="手机号">
            <Input value={form.phone} placeholder="+1..." onChange={(e) => update({ phone: e.target.value })} />
          </Field>
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <Button onClick={() => start.mutate()} disabled={start.isPending || !form.api_id.trim() || !form.api_hash.trim() || !form.phone.trim()}>
            {start.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            发送验证码
          </Button>
        </div>
        {status?.configured && (
          <div className="grid gap-4 md:grid-cols-[1fr_auto]">
            <Field label="验证码">
              <Input value={form.code} onChange={(e) => update({ code: e.target.value })} />
            </Field>
            <div className="flex items-end">
              <Button className="w-full" onClick={() => verify.mutate()} disabled={verify.isPending || !form.code.trim()}>
                {verify.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                验证
              </Button>
            </div>
          </div>
        )}
        {status?.password_needed && (
          <div className="grid gap-4 md:grid-cols-[1fr_auto]">
            <Field label="2FA 密码">
              <Input type="password" value={form.password} onChange={(e) => update({ password: e.target.value })} />
            </Field>
            <div className="flex items-end">
              <Button className="w-full" onClick={() => password.mutate()} disabled={password.isPending || !form.password.trim()}>
                {password.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                完成登录
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ChannelPanel({ channels, loading }: { channels: TGChannel[]; loading: boolean }) {
  const qc = useQueryClient()
  const [identifier, setIdentifier] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const sync = useMutation({
    mutationFn: () => api('/api/tg/channels/sync', { method: 'POST', body: JSON.stringify({}) }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['tg'] })
    },
  })
  const create = useMutation({
    mutationFn: () => api<TGChannel>('/api/tg/channels', { method: 'POST', body: JSON.stringify({ identifier, message_limit: 10 }) }),
    onSuccess: async () => {
      setIdentifier('')
      await qc.invalidateQueries({ queryKey: ['tg', 'channels'] })
    },
  })
  const removeSelected = useMutation({
    mutationFn: async (ids: string[]) => {
      await Promise.all(ids.map((id) => api(`/api/tg/channels/${id}`, { method: 'DELETE' })))
    },
    onSuccess: async () => {
      setSelected(new Set())
      await qc.invalidateQueries({ queryKey: ['tg'] })
    },
  })
  const selectedIds = channels.filter((channel) => selected.has(channel.id)).map((channel) => channel.id)
  const allSelected = channels.length > 0 && selectedIds.length === channels.length
  const toggleSelected = (id: string, checked: boolean) => setSelected((value) => {
    const next = new Set(value)
    if (checked) next.add(id)
    else next.delete(id)
    return next
  })
  const setAllSelected = (checked: boolean) => setSelected(checked ? new Set(channels.map((channel) => channel.id)) : new Set())
  return (
    <Card className="bg-card">
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle>频道管理</CardTitle>
            <CardDescription>公开频道可手动添加，私密频道用同步频道导入</CardDescription>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2">
            {loading && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
            {channels.length > 0 && (
              <>
                <Button variant="outline" size="sm" onClick={() => setAllSelected(!allSelected)}>
                  {allSelected ? '取消全选' : '全选'}
                </Button>
                <Button
                  variant="danger"
                  size="sm"
                  onClick={() => confirmDelete(`选中的 ${selectedIds.length} 个频道`) && removeSelected.mutate(selectedIds)}
                  disabled={removeSelected.isPending || selectedIds.length === 0}
                >
                  {removeSelected.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                  删除选中
                </Button>
                <Button
                  variant="danger"
                  size="sm"
                  onClick={() => confirmDelete('全部频道') && removeSelected.mutate(channels.map((channel) => channel.id))}
                  disabled={removeSelected.isPending}
                >
                  清空全部
                </Button>
              </>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent className="grid gap-4">
        <FormError error={create.error || sync.error} />
        <div className="grid gap-2 md:grid-cols-[1fr_auto]">
          <Input value={identifier} placeholder="@channel 或 https://t.me/channel" onChange={(e) => setIdentifier(e.target.value)} />
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => create.mutate()} disabled={create.isPending || !identifier.trim()}>
              {create.isPending ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
              新增频道
            </Button>
            <Button variant="outline" onClick={() => sync.mutate()} disabled={sync.isPending}>
              {sync.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCcw className="size-4" />}
              同步频道
            </Button>
          </div>
        </div>
        {channels.length === 0 ? <EmptyPanel text="暂无频道" /> : (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {channels.map((channel) => (
              <ChannelCard
                key={channel.id}
                channel={channel}
                selected={selected.has(channel.id)}
                onSelectedChange={(checked) => toggleSelected(channel.id, checked)}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ChannelCard({ channel, selected, onSelectedChange }: { channel: TGChannel; selected: boolean; onSelectedChange: (checked: boolean) => void }) {
  const qc = useQueryClient()
  const [limit, setLimit] = useState(String(channel.message_limit || 10))
  const update = useMutation({
    mutationFn: (patch: Partial<TGChannel>) => api<TGChannel>(`/api/tg/channels/${channel.id}`, { method: 'PATCH', body: JSON.stringify({ ...patch, message_limit: Number(limit) || channel.message_limit }) }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['tg'] })
    },
  })
  const remove = useMutation({
    mutationFn: () => api(`/api/tg/channels/${channel.id}`, { method: 'DELETE' }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['tg'] })
    },
  })
  return (
    <Card className="bg-background">
      <CardContent className="grid gap-3 p-3">
        <div className="flex min-w-0 items-start gap-3">
          <input
            type="checkbox"
            className="mt-1 size-4 accent-primary"
            checked={selected}
            onChange={(event) => onSelectedChange(event.target.checked)}
            aria-label={`选择 ${channel.display_name}`}
          />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="truncate font-medium text-foreground">{channel.display_name}</span>
              <Badge variant={channel.enabled ? 'success' : 'secondary'}>{channel.enabled ? '启用' : '停用'}</Badge>
              {channel.pinned_only && <Badge variant="secondary">只看置顶</Badge>}
              {channel.last_error && <Badge variant="destructive">异常</Badge>}
            </div>
            <div className="mt-1 truncate text-xs text-muted-foreground">{channel.identifier || channel.username || channel.peer_id}</div>
          </div>
          <IconAction title="删除" icon={Trash2} onClick={() => confirmDelete(channel.display_name) && remove.mutate()} pending={remove.isPending} danger />
        </div>
        <div className="grid gap-2 sm:grid-cols-[80px_1fr_92px]">
          <Field label="条数">
            <Input className="h-9" inputMode="numeric" value={limit} onChange={(e) => setLimit(e.target.value)} onBlur={() => update.mutate({})} />
          </Field>
          <Field label="内容">
            <Select value={channel.pinned_only ? 'pinned' : 'all'} onValueChange={(value) => update.mutate({ pinned_only: value === 'pinned' })}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部消息</SelectItem>
                <SelectItem value="pinned">只看置顶</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="状态">
            <Select value={channel.enabled ? 'true' : 'false'} onValueChange={(value) => update.mutate({ enabled: value === 'true' })}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="true">启用</SelectItem>
                <SelectItem value="false">停用</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </div>
      </CardContent>
    </Card>
  )
}

function MessageList({ messages, channels, loading }: { messages: TGMessage[]; channels: TGChannel[]; loading: boolean }) {
  const qc = useQueryClient()
  const [showActions, setShowActions] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const removeSelected = useMutation({
    mutationFn: async (ids: string[]) => {
      await Promise.all(ids.map((id) => api(`/api/tg/messages/${id}`, { method: 'DELETE' })))
    },
    onSuccess: async () => {
      setSelected(new Set())
      await qc.invalidateQueries({ queryKey: ['tg', 'messages'] })
    },
  })
  const clearAll = useMutation({
    mutationFn: () => api('/api/tg/messages', { method: 'DELETE' }),
    onSuccess: async () => {
      setSelected(new Set())
      await qc.invalidateQueries({ queryKey: ['tg', 'messages'] })
    },
  })
  const channelMap = new Map(channels.map((channel) => [channel.id, channel]))
  const timeline = [...messages].sort((a, b) => {
    const time = Date.parse(b.published_at || '') - Date.parse(a.published_at || '')
    return time || b.remote_id - a.remote_id
  })
  const selectedIds = messages.filter((message) => selected.has(message.id)).map((message) => message.id)
  const allSelected = messages.length > 0 && selectedIds.length === messages.length
  const toggleSelected = (id: string, checked: boolean) => setSelected((value) => {
    const next = new Set(value)
    if (checked) next.add(id)
    else next.delete(id)
    return next
  })
  if (loading) return <EmptyPanel text="加载中..." />
  if (messages.length === 0) return <EmptyPanel text="暂无消息" />
  return (
    <div className="grid gap-4">
      <FormError error={removeSelected.error || clearAll.error} />
      <div className="flex justify-end">
        <Button variant="outline" size="sm" onClick={() => setShowActions((value) => !value)}>
          {showActions ? '收起管理' : '管理消息'}
        </Button>
      </div>
      {showActions && (
        <Card className="bg-card">
          <CardContent className="flex flex-wrap items-center justify-between gap-2 py-3">
            <div className="text-sm text-muted-foreground">当前显示 {messages.length} 条消息</div>
            <div className="flex flex-wrap justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setSelected(allSelected ? new Set() : new Set(messages.map((message) => message.id)))}>
                {allSelected ? '取消全选' : '全选消息'}
              </Button>
              <Button
                variant="danger"
                size="sm"
                onClick={() => confirmDelete(`选中的 ${selectedIds.length} 条消息`) && removeSelected.mutate(selectedIds)}
                disabled={removeSelected.isPending || selectedIds.length === 0}
              >
                {removeSelected.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                删除选中
              </Button>
              <Button
                variant="danger"
                size="sm"
                onClick={() => confirmDelete('全部消息缓存') && clearAll.mutate()}
                disabled={clearAll.isPending}
              >
                {clearAll.isPending && <Loader2 className="size-4 animate-spin" />}
                清空消息
              </Button>
            </div>
          </CardContent>
        </Card>
      )}
      <div className="relative grid gap-3 pl-5">
        <div aria-hidden className="absolute bottom-2 left-1.5 top-2 w-px bg-border" />
        {timeline.map((message) => (
          <MessageItem
            key={message.id}
            message={message}
            channel={channelMap.get(message.channel_id)}
            selected={selected.has(message.id)}
            selectionVisible={showActions}
            onSelectedChange={(checked) => toggleSelected(message.id, checked)}
          />
        ))}
      </div>
    </div>
  )
}

function MessageItem({
  message,
  channel,
  selected,
  selectionVisible,
  onSelectedChange,
}: {
  message: TGMessage
  channel?: TGChannel
  selected: boolean
  selectionVisible: boolean
  onSelectedChange: (checked: boolean) => void
}) {
  const channelName = message.channel_name || channel?.display_name || 'Telegram'
  return (
    <div className="relative">
      <div aria-hidden className="absolute -left-5 top-7 size-3 rounded-full border-2 border-background bg-primary" />
      <Card className={`bg-card transition-colors hover:border-primary/35 ${selected ? 'ring-1 ring-primary' : ''}`}>
        <CardContent className="grid gap-3 p-3 sm:p-4">
          <div className="flex min-w-0 items-start justify-between gap-3">
            <div className="flex min-w-0 items-start gap-3">
              <ChannelAvatar name={channelName} url={channel?.avatar_url} />
              <div className="min-w-0">
                <div className="truncate text-sm font-medium text-foreground">{channelName}</div>
                <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <span>{displayTime(message.published_at)}</span>
                  {message.media_type && <Badge variant="secondary">{mediaLabel(message.media_type)}</Badge>}
                  {message.media_type && !message.media_cached && <Badge variant="destructive">媒体未缓存</Badge>}
                </div>
              </div>
            </div>
            {selectionVisible && (
              <input
                type="checkbox"
                className="mt-2 size-4 shrink-0 accent-primary"
                checked={selected}
                onChange={(event) => onSelectedChange(event.target.checked)}
                aria-label="选择消息"
              />
            )}
          </div>
          <HoverText value={message.text || '(无文本)'} className="text-sm leading-[1.6] text-body" alwaysTooltip>
            <div data-hover-text className="line-clamp-5 whitespace-pre-wrap break-words">{message.text || '(无文本)'}</div>
          </HoverText>
          <MediaPreview message={message} />
        </CardContent>
      </Card>
    </div>
  )
}

function ChannelAvatar({ name, url }: { name: string; url?: string }) {
  const [failed, setFailed] = useState(false)
  if (url && !failed) {
    return <img src={url} alt="" className="size-10 shrink-0 rounded-full border border-border bg-background object-cover" onError={() => setFailed(true)} />
  }
  return (
    <div className="grid size-10 shrink-0 place-items-center rounded-full bg-primary text-sm font-medium text-primary-foreground">
      {avatarText(name)}
    </div>
  )
}

function MediaPreview({ message }: { message: TGMessage }) {
  if (!message.media_type) return null
  if (!message.media_cached || !message.media_url) {
    return <div className="grid min-h-36 place-items-center rounded-sm border border-dashed border-border text-xs text-muted-foreground">媒体未缓存</div>
  }
  if (message.media_type === 'photo' || message.media_type === 'image' || message.media_url.endsWith('.jpg')) {
    return (
      <Dialog>
        <DialogTrigger asChild>
          <button type="button" className="block overflow-hidden rounded-sm border border-border bg-background text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/30">
            <img src={message.media_url} alt="" className="h-40 w-full object-cover sm:h-44" />
          </button>
        </DialogTrigger>
        <DialogContent className="max-w-[960px] bg-background">
          <DialogTitle>图片预览</DialogTitle>
          <div className="overflow-hidden rounded-sm border border-border bg-card">
            <img src={message.media_url} alt="" className="max-h-[70vh] w-full object-contain" />
          </div>
          <div className="flex justify-end">
            <Button asChild variant="outline" size="sm">
              <a href={message.media_url} target="_blank" rel="noreferrer">打开原图</a>
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    )
  }
  if (message.media_type === 'video') {
    return <video src={message.media_url} controls className="h-40 w-full rounded-sm border border-border bg-background object-cover sm:h-44" />
  }
  return (
    <Button asChild variant="outline" size="sm" className="self-start">
      <a href={message.media_url} target="_blank" rel="noreferrer">打开媒体</a>
    </Button>
  )
}

function mediaLabel(type: string) {
  return ({ photo: '图片', image: '图片', video: '视频', audio: '音频', document: '文件' } as Record<string, string>)[type] ?? type
}

function displayTime(value?: string) {
  if (!value || value.startsWith('0001-')) return '-'
  return fmtTime(value)
}

function avatarText(value: string) {
  return value.trim().replace(/^@/, '').slice(0, 1).toUpperCase() || 'T'
}

function confirmDelete(name: string) {
  return window.confirm(`确认删除 ${name}？`)
}
