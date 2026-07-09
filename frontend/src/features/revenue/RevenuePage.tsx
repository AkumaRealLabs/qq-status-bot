import { useState, type ReactNode } from 'react'
import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { arrayMove, rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { GripVertical, Loader2, Pencil, Plus, RefreshCcw, Save, Trash2 } from 'lucide-react'
import { EmptyPanel, Field, FormError, HoverText, IconAction, SkeletonCardGrid, StatusBadge } from '@/components/common'
import { Page } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage, fmtTime, num } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { RevenueCard, RevenueCardForm, RevenueOrder, RevenueRow } from '@/types'

const emptyRevenueForm: RevenueCardForm = {
  name: '',
  source_type: 'epay_total',
  base_url: '',
  user_id: '',
  access_token: '',
  admin_api_key: '',
  epay_pid: '',
  epay_key: '',
  enabled: true,
}

export function RevenuePage() {
  const qc = useQueryClient()
  const [layoutEditing, setLayoutEditing] = useState(false)
  const [draftRows, setDraftRows] = useState<RevenueRow[]>([])
  const q = useQuery({ queryKey: ['revenue', 'today'], queryFn: () => api<RevenueRow[]>('/api/revenue/today'), refetchInterval: 60000 })
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
          {q.isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
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
          <RevenueCardDialog />
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
                {shownRows.map((row) => <SortableRevenueCard key={row.id} row={row} sorting={sortCards.isPending} />)}
              </div>
            </SortableContext>
          </DndContext>
        ) : (
          <div className="grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3">
            {shownRows.map((row) => <RevenueCardView key={row.id} row={row} />)}
          </div>
        )
      )}
      {!q.isLoading && !q.isError && rows.length === 0 && <EmptyPanel text="暂无收入卡片" />}
    </Page>
  )
}

function SortableRevenueCard({
  row,
  sorting,
}: {
  row: RevenueRow
  sorting: boolean
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: row.id })
  return (
    <div ref={setNodeRef} className={cn('min-w-0', isDragging && 'z-10 opacity-80')} style={{ transform: CSS.Transform.toString(transform), transition }}>
      <RevenueCardView
        row={row}
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
  dragHandle,
}: {
  row: RevenueRow
  dragHandle?: ReactNode
}) {
  const [ordersOpen, setOrdersOpen] = useState(false)
  const ok = row.enabled && !row.error
  const badgeText = !row.enabled ? '停用' : row.error ? '异常' : '正常'
  return (
    <>
      <Card
        role="button"
        tabIndex={0}
        className={cn('min-w-0 cursor-pointer bg-card transition-colors hover:border-primary/45 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30', row.error && 'border-destructive/40')}
        onClick={() => setOrdersOpen(true)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            setOrdersOpen(true)
          }
        }}
      >
        <CardHeader className="gap-2">
          <div className="flex min-w-0 items-start justify-between gap-3">
            <div className="min-w-0">
              <CardTitle className="truncate">{row.name}</CardTitle>
              <CardDescription>{sourceLabel(row.source_type)}</CardDescription>
            </div>
            <div className="flex shrink-0 flex-col items-end gap-1.5">
              <StatusBadge ok={ok} okText={badgeText} failText={badgeText} />
              <div className="flex flex-wrap justify-end gap-1.5" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
                {dragHandle}
                <RevenueCardDialog card={row} />
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent className="grid gap-3">
          <div>
            <div className="text-sm text-muted-foreground">收入金额</div>
            <div className="break-words font-display text-3xl font-normal">{row.enabled ? `${num(row.revenue)} 元` : '-'}</div>
            <div className="mt-1.5 text-xs text-muted-foreground">最后刷新：{displayTime(row.checked_at)}</div>
          </div>
          {row.error && <HoverText value={row.error} className="rounded-sm bg-destructive/10 px-2.5 py-1.5 text-xs text-destructive" alwaysTooltip />}
        </CardContent>
      </Card>
      <RevenueOrdersDialog row={row} open={ordersOpen} setOpen={setOrdersOpen} />
    </>
  )
}

function RevenueOrdersDialog({ row, open, setOpen }: { row: RevenueRow; open: boolean; setOpen: (open: boolean) => void }) {
  const q = useQuery({
    queryKey: ['revenue', 'orders', row.id],
    queryFn: () => api<RevenueOrder[]>(`/api/revenue/cards/${row.id}/orders`),
    enabled: open && row.enabled,
  })
  const orders = q.data ?? []
  const total = orders.reduce((sum, order) => sum + order.amount, 0)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-w-[860px]">
        <DialogTitle>{row.name}</DialogTitle>
        {!row.enabled ? (
          <div className="rounded-sm border border-border bg-card px-3 py-8 text-center text-sm text-muted-foreground">卡片已停用</div>
        ) : (
          <>
            <FormError error={q.error} />
            <div className="flex flex-wrap gap-3 text-sm text-muted-foreground">
              <span>订单数：{orders.length}</span>
              <span>金额：{num(total)} 元</span>
            </div>
            {q.isLoading && <div className="rounded-sm border border-border bg-card px-3 py-8 text-center text-sm text-muted-foreground">加载中...</div>}
            {!q.isLoading && !q.isError && orders.length === 0 && <div className="rounded-sm border border-border bg-card px-3 py-8 text-center text-sm text-muted-foreground">暂无今日成功订单</div>}
            {!q.isLoading && !q.isError && orders.length > 0 && (
              <div className="overflow-x-auto rounded-sm border border-border">
                <table className="w-full min-w-[640px] text-left text-sm">
                  <thead className="bg-secondary text-xs text-muted-foreground">
                    <tr>
                      <th className="px-3 py-2 font-medium">订单号</th>
                      <th className="px-3 py-2 font-medium">金额</th>
                      <th className="px-3 py-2 font-medium">方式</th>
                      <th className="px-3 py-2 font-medium">状态</th>
                      <th className="px-3 py-2 font-medium">完成时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {orders.map((order, index) => (
                      <tr key={`${order.remote_id}-${index}`} className="border-t border-border">
                        <td className="max-w-64 truncate px-3 py-2 font-mono text-xs">{order.remote_id || '-'}</td>
                        <td className="px-3 py-2">{num(order.amount)} 元</td>
                        <td className="px-3 py-2">{order.payment_type || '-'}</td>
                        <td className="px-3 py-2">{order.status || '-'}</td>
                        <td className="px-3 py-2">{displayTime(order.paid_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

function RevenueCardDialog({ card }: { card?: RevenueCard }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<RevenueCardForm>(() => cardToForm(card))
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
            <Select value={form.source_type} onValueChange={(value) => update({ source_type: value as RevenueCardForm['source_type'] })}>
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
          <Field label="Base URL">
            <Input value={form.base_url} placeholder={baseURLPlaceholder(form.source_type)} onChange={(e) => update({ base_url: e.target.value })} />
          </Field>
          {form.source_type === 'epay_total' && (
            <>
              <Field label="PID">
                <Input value={form.epay_pid} onChange={(e) => update({ epay_pid: e.target.value })} />
              </Field>
              <Field label="Key">
                <Input
                  type="password"
                  value={form.epay_key}
                  placeholder={card?.epay_key_set ? '已保存，不修改请留空' : ''}
                  onChange={(e) => update({ epay_key: e.target.value })}
                />
              </Field>
            </>
          )}
          {form.source_type === 'newapi_orders' && (
            <>
              <Field label="管理员用户 ID">
                <Input value={form.user_id} onChange={(e) => update({ user_id: e.target.value })} />
              </Field>
              <Field label="管理员 Access Token">
                <Input
                  type="password"
                  value={form.access_token}
                  placeholder={card?.access_token_set ? '已保存，不修改请留空' : ''}
                  onChange={(e) => update({ access_token: e.target.value })}
                />
              </Field>
            </>
          )}
          {form.source_type === 'sub2api_orders' && (
            <Field label="管理员 API Key">
              <Input
                type="password"
                value={form.admin_api_key}
                placeholder={card?.admin_api_key_set ? '已保存，不修改请留空' : 'admin-...'}
                onChange={(e) => update({ admin_api_key: e.target.value })}
              />
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
          <Button onClick={() => save.mutate()} disabled={save.isPending || !formReady(form, card)}>
            {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            保存
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function sourceLabel(type: RevenueCard['source_type']) {
  return ({ epay_total: '总收入', newapi_orders: 'new-api 订单', sub2api_orders: 'sub2api 订单' } as const)[type] ?? type
}

function baseURLPlaceholder(type: RevenueCard['source_type']) {
  return type === 'epay_total' ? 'https://pay.example.com' : 'https://example.com'
}

function cardToForm(card?: RevenueCard): RevenueCardForm {
  if (!card) return emptyRevenueForm
  return {
    name: card.name || '',
    source_type: card.source_type,
    base_url: card.base_url || '',
    user_id: card.user_id || '',
    access_token: card.access_token || '',
    admin_api_key: card.admin_api_key || '',
    epay_pid: card.epay_pid || '',
    epay_key: card.epay_key || '',
    enabled: card.enabled,
  }
}

function cardPayload(form: RevenueCardForm) {
  return {
    name: form.name,
    source_type: form.source_type,
    base_url: form.base_url,
    user_id: form.source_type === 'newapi_orders' ? form.user_id : '',
    access_token: form.source_type === 'newapi_orders' ? form.access_token : '',
    admin_api_key: form.source_type === 'sub2api_orders' ? form.admin_api_key : '',
    epay_pid: form.source_type === 'epay_total' ? form.epay_pid : '',
    epay_key: form.source_type === 'epay_total' ? form.epay_key : '',
    upstream_id: '',
    enabled: form.enabled,
  }
}

function formReady(form: RevenueCardForm, card?: RevenueCard) {
  if (!form.name.trim()) return false
  if (!form.base_url.trim() && !card?.base_url) return false
  if (form.source_type === 'epay_total') {
    return Boolean((form.epay_pid.trim() || card?.epay_pid) && (form.epay_key.trim() || card?.epay_key_set))
  }
  if (form.source_type === 'newapi_orders') {
    return Boolean((form.user_id.trim() || card?.user_id) && (form.access_token.trim() || card?.access_token_set))
  }
  if (form.source_type === 'sub2api_orders') return Boolean(form.admin_api_key.trim() || card?.admin_api_key_set)
  return false
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
