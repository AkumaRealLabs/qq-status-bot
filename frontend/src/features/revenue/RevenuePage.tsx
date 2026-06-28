import { useState, type ReactNode } from 'react'
import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { arrayMove, rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { GripVertical, Loader2, Pencil, Plus, RefreshCcw, Save, Settings, Trash2 } from 'lucide-react'
import { EmptyPanel, Field, FormError, HoverText, IconAction, MiniStat, SkeletonCardGrid, StatusBadge } from '@/components/common'
import { Page } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage, fmtTime, num } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { RevenueCard, RevenueCardForm, RevenueRow, UpstreamRow } from '@/types'

const emptyRevenueForm: RevenueCardForm = {
  name: '',
  source_type: 'epay_total',
  upstream_id: '',
  enabled: true,
}

export function RevenuePage({ onOpenSettings }: { onOpenSettings: () => void }) {
  const qc = useQueryClient()
  const [layoutEditing, setLayoutEditing] = useState(false)
  const [draftRows, setDraftRows] = useState<RevenueRow[]>([])
  const q = useQuery({ queryKey: ['revenue', 'today'], queryFn: () => api<RevenueRow[]>('/api/revenue/today'), refetchInterval: 60000 })
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamRow[]>('/api/upstreams') })
  const rows = q.data ?? []
  const shownRows = layoutEditing ? draftRows : rows
  const sortDirty = !sameIDs(rows, draftRows)
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
  const sortCards = useMutation({
    mutationFn: (ids: string[]) => api('/api/revenue/cards/order', { method: 'POST', body: JSON.stringify({ ids }) }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['revenue'] })
    },
    onError: (error) => window.alert(errorMessage(error)),
  })
  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event
    if (!over || active.id === over.id) return
    const oldIndex = draftRows.findIndex((row) => row.id === active.id)
    const newIndex = draftRows.findIndex((row) => row.id === over.id)
    if (oldIndex < 0 || newIndex < 0) return
    setDraftRows((value) => arrayMove(value, oldIndex, newIndex))
  }
  const saveSort = () => {
    sortCards.mutate(draftRows.map((row) => row.id).filter(Boolean), { onSuccess: () => setLayoutEditing(false) })
  }
  return (
    <Page
      title="今日收入"
      description="收入卡片和来源明细"
      actions={
        <div className="flex max-w-full flex-wrap items-center justify-end gap-2">
          {(q.isFetching || upstreams.isFetching) && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
          <Button variant="outline" size="sm" onClick={() => void q.refetch()} disabled={q.isFetching}>
            <RefreshCcw className={cn('size-4', q.isFetching && 'animate-spin')} />
            刷新
          </Button>
          {layoutEditing ? (
            <>
              <Button variant="outline" size="sm" onClick={() => { setDraftRows(rows); setLayoutEditing(false) }} disabled={sortCards.isPending}>取消</Button>
              <Button size="sm" onClick={saveSort} disabled={!sortDirty || sortCards.isPending}>
                {sortCards.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                保存排序
              </Button>
            </>
          ) : (
            rows.length > 0 && <Button variant="outline" size="sm" onClick={() => { setDraftRows(rows); setLayoutEditing(true) }}>修改布局</Button>
          )}
          <RevenueCardDialog rows={upstreams.data ?? []} />
        </div>
      }
    >
      {q.isLoading && <SkeletonCardGrid count={3} />}
      {q.isError && <EmptyPanel text={errorMessage(q.error)} />}
      {!q.isLoading && !q.isError && shownRows.length > 0 && (
        layoutEditing ? (
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={shownRows.map((row) => row.id)} strategy={rectSortingStrategy}>
              <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
                {shownRows.map((row) => <SortableRevenueCard key={row.id} row={row} rows={upstreams.data ?? []} sorting={sortCards.isPending} onOpenSettings={onOpenSettings} />)}
              </div>
            </SortableContext>
          </DndContext>
        ) : (
          <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
            {shownRows.map((row) => <RevenueCardView key={row.id} row={row} rows={upstreams.data ?? []} onOpenSettings={onOpenSettings} />)}
          </div>
        )
      )}
      {!q.isLoading && !q.isError && rows.length === 0 && <EmptyPanel text="暂无收入卡片" />}
    </Page>
  )
}

function SortableRevenueCard({
  row,
  rows,
  sorting,
  onOpenSettings,
}: {
  row: RevenueRow
  rows: UpstreamRow[]
  sorting: boolean
  onOpenSettings: () => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: row.id })
  return (
    <div ref={setNodeRef} className={cn('min-w-0', isDragging && 'z-10 opacity-80')} style={{ transform: CSS.Transform.toString(transform), transition }}>
      <RevenueCardView
        row={row}
        rows={rows}
        onOpenSettings={onOpenSettings}
        dragHandle={
          <Button variant="outline" size="icon" title="拖拽排序" disabled={sorting} className="touch-none" {...attributes} {...listeners}>
            <GripVertical className="size-4" />
            <span className="sr-only">拖拽排序</span>
          </Button>
        }
      />
    </div>
  )
}

function RevenueCardView({
  row,
  rows,
  dragHandle,
  onOpenSettings,
}: {
  row: RevenueRow
  rows: UpstreamRow[]
  dragHandle?: ReactNode
  onOpenSettings: () => void
}) {
  const ok = row.enabled && !row.error
  const badgeText = !row.enabled ? '停用' : row.error ? '异常' : '正常'
  return (
    <Card className={cn('min-w-0 bg-card', row.error && 'border-destructive/40')}>
      <CardHeader className="min-h-16 gap-2 border-b border-border">
        <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
          <div className="min-w-0 pt-1">
            <CardTitle className="break-words text-lg leading-tight">{row.name}</CardTitle>
            <CardDescription className="mt-1.5 text-xs leading-relaxed">{cardDescription(row)}</CardDescription>
          </div>
          <div className="flex shrink-0 flex-col items-end gap-1.5">
            <StatusBadge ok={ok} okText={badgeText} failText={badgeText} />
            <div className="flex flex-wrap justify-end gap-1.5">
              {dragHandle}
              <RevenueCardDialog rows={rows} card={row} />
              {row.source_type === 'epay_total' && row.error && (
                <Button variant="outline" size="icon" onClick={onOpenSettings} title="设置">
                  <Settings className="size-4" />
                  <span className="sr-only">设置</span>
                </Button>
              )}
            </div>
          </div>
        </div>
      </CardHeader>
      <CardContent className="grid gap-2.5 pt-2.5">
        <div className="font-display break-words text-3xl font-normal">{row.enabled ? `${num(row.revenue)} 元` : '-'}</div>
        <div className="grid min-w-0 grid-cols-2 gap-2">
          <MiniStat label="来源" value={sourceLabel(row.source_type)} />
          <MiniStat label="刷新时间" value={displayTime(row.checked_at)} />
        </div>
        {row.error && <HoverText value={row.error} className="rounded-sm bg-destructive/10 px-2.5 py-1.5 text-xs text-destructive" alwaysTooltip />}
      </CardContent>
    </Card>
  )
}

function RevenueCardDialog({ rows, card }: { rows: UpstreamRow[]; card?: RevenueCard }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<RevenueCardForm>(() => cardToForm(card))
  const upstreamType = sourceUpstreamType(form.source_type)
  const upstreamOptions = upstreamType ? rows.filter((row) => row.upstream.type === upstreamType) : []
  const save = useMutation({
    mutationFn: () =>
      api(card ? `/api/revenue/cards/${card.id}` : '/api/revenue/cards', {
        method: card ? 'PATCH' : 'POST',
        body: JSON.stringify(cardPayload(form)),
      }),
    onSuccess: async () => {
      setOpen(false)
      await qc.invalidateQueries({ queryKey: ['revenue'] })
    },
  })
  const remove = useMutation({
    mutationFn: () => api(`/api/revenue/cards/${card?.id ?? ''}`, { method: 'DELETE' }),
    onSuccess: async () => {
      setOpen(false)
      await qc.invalidateQueries({ queryKey: ['revenue'] })
    },
    onError: (error) => window.alert(errorMessage(error)),
  })
  const update = (patch: Partial<RevenueCardForm>) => setForm((value) => ({ ...value, ...patch }))
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
            <Plus className="size-4" />
            新增卡片
          </Button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogTitle>{card ? '编辑收入卡片' : '新增收入卡片'}</DialogTitle>
        <FormError error={save.error} />
        <div className="grid min-w-0 gap-4 md:grid-cols-2">
          <Field label="名称">
            <Input value={form.name} onChange={(e) => update({ name: e.target.value })} />
          </Field>
          <Field label="类型">
            <Select
              value={form.source_type}
              onValueChange={(value) => update({ source_type: value as RevenueCardForm['source_type'], upstream_id: '' })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="epay_total">总收入</SelectItem>
                <SelectItem value="newapi_orders">new-api 订单</SelectItem>
                <SelectItem value="sub2api_orders">sub2api 订单</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {upstreamType && (
            <Field label="上游">
              <Select value={form.upstream_id} onValueChange={(value) => update({ upstream_id: value })}>
                <SelectTrigger>
                  <SelectValue placeholder="选择上游" />
                </SelectTrigger>
                <SelectContent>
                  {upstreamOptions.map((row) => (
                    <SelectItem key={row.upstream.id} value={row.upstream.id}>
                      {row.upstream.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
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
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          {card && <IconAction title="删除" onClick={() => confirmDelete(card.name) && remove.mutate()} pending={remove.isPending} icon={Trash2} danger />}
          <Button onClick={() => save.mutate()} disabled={save.isPending || !formReady(form)}>
            {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            保存
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function cardDescription(row: RevenueCard) {
  return row.upstream_name ? `${sourceLabel(row.source_type)} · ${row.upstream_name}` : sourceLabel(row.source_type)
}

function sourceLabel(type: RevenueCard['source_type']) {
  return ({ epay_total: '总收入', newapi_orders: 'new-api 订单', sub2api_orders: 'sub2api 订单' } as const)[type] ?? type
}

function sourceUpstreamType(type: RevenueCard['source_type']) {
  if (type === 'newapi_orders') return 'newapi'
  if (type === 'sub2api_orders') return 'sub2api'
  return ''
}

function cardToForm(card?: RevenueCard): RevenueCardForm {
  if (!card) return emptyRevenueForm
  return {
    name: card.name || '',
    source_type: card.source_type,
    upstream_id: card.upstream_id || '',
    enabled: card.enabled,
  }
}

function cardPayload(form: RevenueCardForm) {
  return {
    name: form.name,
    source_type: form.source_type,
    upstream_id: sourceUpstreamType(form.source_type) ? form.upstream_id : '',
    enabled: form.enabled,
  }
}

function formReady(form: RevenueCardForm) {
  if (!form.name.trim()) return false
  return !sourceUpstreamType(form.source_type) || Boolean(form.upstream_id)
}

function sameIDs(a: RevenueRow[], b: RevenueRow[]) {
  return a.length === b.length && a.every((item, index) => item.id === b[index]?.id)
}

function displayTime(value?: string) {
  if (!value || value.startsWith('0001-')) return '-'
  return fmtTime(value)
}

function confirmDelete(name: string) {
  return window.confirm(`确认删除 ${name}？`)
}
