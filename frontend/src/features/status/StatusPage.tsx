import { useState, type ReactNode } from 'react'
import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { arrayMove, rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { GripVertical, KeyRound, Loader2, Pencil, RefreshCcw, Save, Trash2 } from 'lucide-react'
import { EmptyPanel, Field, FormError, HoverText, IconAction, Metric, MiniStat, SkeletonCardGrid, StatusBadge, WindowSelect } from '@/components/common'
import { BrandIcon, Page } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage, fmtTime } from '@/lib/format'
import { useAutoClear } from '@/lib/hooks'
import { invalidateMonitor } from '@/lib/query'
import { cn } from '@/lib/utils'
import type { CardForm, ModelCard, MonitorStatus, Probe, PublicModelCard, PublicMonitorStatus, SiteSettings, UpstreamRow } from '@/types'

const emptyCardForm: CardForm = {
  name: '',
  source: 'custom',
  base_url: '',
  api_key: '',
  upstream_id: '',
  key_id: '',
  display_group: '',
  enabled: true,
  public_enabled: false,
}

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

export function AdminStatusPage() {
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
  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event
    if (!over || active.id === over.id) return
    const oldIndex = draftCards.findIndex((card) => card.id === active.id)
    const newIndex = draftCards.findIndex((card) => card.id === over.id)
    if (oldIndex < 0 || newIndex < 0) return
    setDraftCards((value) => arrayMove(value, oldIndex, newIndex))
  }
  const saveSort = () => {
    sortCards.mutate(draftCards.map((card) => card.id).filter(Boolean), { onSuccess: () => setLayoutEditing(false) })
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
          <CardDialog rows={upstreams.data ?? []} cards={cards} />
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
                {shownCards.map((card) => <SortableStatusCard key={card.id} card={card} windowValue={windowValue} rows={upstreams.data ?? []} cards={cards} sorting={sortCards.isPending} />)}
              </div>
            </SortableContext>
          </DndContext>
        ) : (
          <StatusCardGroups cards={shownCards} render={(card) => <StatusMonitorCard key={card.id} card={card} windowValue={windowValue} rows={upstreams.data ?? []} cards={cards} />} />
        )
      )}
      {!q.isLoading && cards.length === 0 && <EmptyPanel text="暂无卡片" />}
    </Page>
  )
}

function StatusCardGroups<T extends ModelCard | PublicModelCard>({
  cards,
  render,
}: {
  cards: T[]
  render: (card: T, index: number) => ReactNode
}) {
  return (
    <div className="grid min-w-0 gap-5">
      {groupCards(cards).map((group) => (
        <section key={group.name} className="grid min-w-0 gap-2">
          <div className="text-sm font-medium text-foreground">{group.name}</div>
          <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
            {group.cards.map(render)}
          </div>
        </section>
      ))}
    </div>
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
  cards,
  sorting,
}: {
  card: ModelCard
  windowValue: string
  rows: UpstreamRow[]
  cards: ModelCard[]
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
        cards={cards}
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
  cards = [],
  publicView,
  dragHandle,
}: {
  card: ModelCard | PublicModelCard
  windowValue: string
  rows?: UpstreamRow[]
  cards?: ModelCard[]
  publicView?: boolean
  dragHandle?: ReactNode
}) {
  const qc = useQueryClient()
  const [message, setMessage] = useState('')
  useAutoClear(message, '检查完成', setMessage)
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
  const displayGroup = cardDisplayGroup(card)
  const ratio = editableCard?.effective_ratio || editableCard?.key_group_ratio || '-'
  return (
    <Card className={cn('min-w-0 bg-card', !ok && 'border-destructive/40')}>
      <CardHeader className="min-h-16 gap-2 border-b border-border">
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
          <div className="min-w-0 pt-1">
            <CardTitle className="break-words text-lg leading-tight">{cardTitle(card, ratio)}</CardTitle>
            {!publicView && (
              <CardDescription className="mt-1.5 grid gap-0.5 text-xs leading-relaxed">
                <span>展示分组：{displayGroup}</span>
                <span>Key 分组：{groupName}</span>
                <span>模型：gpt-5.5</span>
              </CardDescription>
            )}
          </div>
          <div className="flex shrink-0 flex-col items-end gap-1.5">
            <StatusBadge ok={ok} okText={statusText} failText={statusText} />
            {!publicView && editableCard && (
              <div className="flex flex-wrap justify-end gap-1.5">
                {dragHandle}
                <CardDialog rows={rows} cards={cards} card={editableCard} />
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

function CardDialog({ rows, cards, card }: { rows: UpstreamRow[]; cards: ModelCard[]; card?: ModelCard }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<CardForm>(() => cardToForm(card))
  const keys = keysOf(rows.find((row) => row.upstream.id === form.upstream_id))
  const displayGroups = existingDisplayGroups(cards)
  const displayGroupListID = card ? `display-groups-${card.id}` : 'display-groups-new'
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
          <Field label="展示分组">
            <Input list={displayGroupListID} value={form.display_group} placeholder="留空归到其他" onChange={(e) => update({ display_group: e.target.value })} />
            {displayGroups.length > 0 && (
              <datalist id={displayGroupListID}>
                {displayGroups.map((group) => <option key={group} value={group} />)}
              </datalist>
            )}
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
        <div className="flex flex-wrap justify-end gap-2">
          {card && <IconAction title="删除" onClick={() => confirmDelete(card.name, '只删除卡片，历史探测记录会保留。') && remove.mutate()} pending={remove.isPending} icon={Trash2} danger />}
          <Button onClick={() => save.mutate()} disabled={save.isPending || !cardFormReady(form)}>
            {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            保存
          </Button>
        </div>
      </DialogContent>
    </Dialog>
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
    ['测什么', probePurpose(probe)],
    ['测试题目', probe.input || '-'],
    ['期望答案', probe.expected_answer || '-'],
    ['模型回答', probe.output || '-'],
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
            <span className={cn('whitespace-pre-wrap break-words text-sm leading-[1.55] text-foreground', tone)}>{value}</span>
          </span>
        ))}
      </span>
    </span>
  )
}

function probeHoverTitle(probe: Probe) {
  const ok = probeOK(probe)
  return `状态：${probeStatusLabel(probeStatus(probe))}\n延迟：${probe.latency_ms} ms\nHTTP 状态：${probe.http_status || '-'}\n测什么：${probePurpose(probe)}\n测试题目：${probe.input || '-'}\n期望答案：${probe.expected_answer || '-'}\n模型回答：${probe.output || '-'}\n模型验证：${ok ? '通过' : '未通过'}\n检查时间：${fmtTime(probe.checked_at)}`
}

function probePurpose(probe: Probe) {
  return probe.expected_answer ? '检查 gpt-5.5 是否能按题目返回指定的一词答案' : '检查 gpt-5.5 响应与连通性'
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
    display_group: card.display_group || '',
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
    display_group: form.display_group,
    enabled: form.enabled,
    public_enabled: form.public_enabled,
  }
}

function groupCards<T extends ModelCard | PublicModelCard>(cards: T[]) {
  const groups: { name: string; cards: T[] }[] = []
  for (const card of cards) {
    const name = cardDisplayGroup(card)
    let group = groups.find((item) => item.name === name)
    if (!group) {
      group = { name, cards: [] }
      groups.push(group)
    }
    group.cards.push(card)
  }
  return groups
}

function existingDisplayGroups(cards: ModelCard[]) {
  return Array.from(new Set(cards.map((card) => card.display_group.trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
}

function cardDisplayGroup(card: ModelCard | PublicModelCard) {
  return card.display_group?.trim() || '其他'
}

function cardFormReady(form: CardForm) {
  if (!form.name.trim()) return false
  return form.source === 'custom' ? Boolean(form.base_url.trim() && form.api_key.trim()) : Boolean(form.upstream_id && form.key_id)
}

function confirmDelete(name: string, note?: string) {
  return window.confirm(`确认删除 ${name}？${note ? `\n${note}` : ''}`)
}
