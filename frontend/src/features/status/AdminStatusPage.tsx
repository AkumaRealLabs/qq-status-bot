import { useState, type ReactNode } from 'react'
import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { arrayMove, rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { GripVertical, KeyRound, Loader2, Pencil, RefreshCcw, Save, Trash2 } from 'lucide-react'
import { EmptyPanel, Field, FormError, IconAction, SkeletonCardGrid, WindowSelect } from '@/components/common'
import { Page } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage } from '@/lib/format'
import { invalidateMonitor } from '@/lib/query'
import { cn } from '@/lib/utils'
import type { CardForm, ModelCard, MonitorStatus, UpstreamRow } from '@/types'
import { StatusCardGroups, StatusMonitorCard, StatusSummary } from './shared'


const emptyCardForm: CardForm = {
  name: '',
  source: 'custom',
  base_url: '',
  api_key: '',
  upstream_id: '',
  key_id: '',
  display_group: '',
  pool_enabled: true,
  manual_cost_ratio: '',
  enabled: true,
  public_enabled: false,
}

function keysOf(row: UpstreamRow | undefined) {
  return row?.keys ?? []
}

function sameIDs(a: ModelCard[], b: ModelCard[]) {
  return a.length === b.length && a.every((item, index) => item.id === b[index]?.id)
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
    pool_enabled: card.pool_enabled ?? true,
    manual_cost_ratio: card.manual_cost_ratio || '',
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
    pool_enabled: form.pool_enabled,
    manual_cost_ratio: form.source === 'custom' && form.pool_enabled ? form.manual_cost_ratio : '',
    enabled: form.enabled,
    public_enabled: form.public_enabled,
  }
}

function existingDisplayGroups(cards: ModelCard[]) {
  return Array.from(new Set(cards.map((card) => card.display_group.trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
}

function cardFormReady(form: CardForm, card?: ModelCard) {
  if (!form.name.trim()) return false
  if (form.source === 'custom') {
    return Boolean(form.base_url.trim() && (form.api_key.trim() || card?.api_key_set))
  }
  return Boolean(form.upstream_id && form.key_id)
}

function confirmDelete(name: string, note?: string) {
  return window.confirm(`确认删除 ${name}？${note ? `\n${note}` : ''}`)
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
          <StatusCardGroups cards={shownCards} render={(card) => <AdminStatusMonitorCard key={card.id} card={card} windowValue={windowValue} rows={upstreams.data ?? []} cards={cards} />} />
        )
      )}
      {!q.isLoading && cards.length === 0 && <EmptyPanel text="暂无卡片" />}
    </Page>
  )
}

function AdminStatusMonitorCard({
  card,
  windowValue,
  rows,
  cards,
  dragHandle,
}: {
  card: ModelCard
  windowValue: string
  rows: UpstreamRow[]
  cards: ModelCard[]
  dragHandle?: ReactNode
}) {
  const qc = useQueryClient()
  const [message, setMessage] = useState('')
  const check = useMutation({
    mutationFn: () => api(`/api/cards/${card.id}/check`, { method: 'POST' }),
    onMutate: () => setMessage('检查中...'),
    onSuccess: async () => {
      setMessage('检查完成')
      await qc.invalidateQueries({ queryKey: ['status'] })
    },
    onError: (error) => setMessage(errorMessage(error)),
  })
  return (
    <StatusMonitorCard
      card={card}
      windowValue={windowValue}
      footerMessage={message}
      adminActions={
        <div className="flex flex-wrap justify-end gap-1.5">
          {dragHandle}
          <CardDialog rows={rows} cards={cards} card={card} />
          <Button variant="outline" size="icon" onClick={() => check.mutate()} disabled={check.isPending} title="检查">
            {check.isPending ? <Loader2 className="size-3 animate-spin" /> : <RefreshCcw className="size-3" />}
            <span className="sr-only">检查</span>
          </Button>
        </div>
      }
    />
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
      <AdminStatusMonitorCard
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
            <Select value={form.source} onValueChange={(value) => update({ source: value as CardForm['source'], key_id: value === 'custom' ? '' : form.key_id, manual_cost_ratio: value === 'custom' ? form.manual_cost_ratio : '' })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="custom">自定义</SelectItem>
                <SelectItem value="upstream">选择上游 Key</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="用途">
            <Select value={form.pool_enabled ? 'pool' : 'monitor'} onValueChange={(value) => update({ pool_enabled: value === 'pool' })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="pool">号池</SelectItem>
                <SelectItem value="monitor">纯监控</SelectItem>
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
                <Input
                  type="password"
                  value={form.api_key}
                  placeholder={card?.api_key_set ? '已保存，不修改请留空' : ''}
                  onChange={(e) => update({ api_key: e.target.value })}
                />
              </Field>
              {form.pool_enabled && (
                <Field label="手动成本">
                  <Input type="number" min="0" step="0.01" value={form.manual_cost_ratio} onChange={(e) => update({ manual_cost_ratio: e.target.value })} />
                </Field>
              )}
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
          <Button onClick={() => save.mutate()} disabled={save.isPending || !cardFormReady(form, card)}>
            {save.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            保存
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}


