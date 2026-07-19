import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Loader2, Pencil, RefreshCcw, Save, Trash2 } from 'lucide-react'
import { ActionRow, EmptyPanel, Field, FeedbackBanner, IconAction, SaveButton, SkeletonCardGrid, WindowSelect } from '@/components/common'
import { Page } from '@/components/layout'
import { DragHandle, SortableGrid, SortableItem } from '@/components/sortable'
import { reorderByDragEnd, type DragEndEvent } from '@/lib/sortable'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { errorMessage } from '@/lib/format'
import { alertError, closeAfterSave, confirmDelete, secretPlaceholder, useFeedback } from '@/lib/feedback'
import { invalidateMonitor } from '@/lib/query'
import { keysOf, sameIDs } from '@/lib/utils'
import type { CardForm, ModelCard, MonitorStatus, UpstreamRow } from '@/types'
import { StatusCardGroups, StatusMonitorCard, StatusSummary } from './shared'


const emptyCardForm: CardForm = {
  name: '',
  source: 'custom',
  base_url: '',
  api_key: '',
  model: 'gpt-5.6-sol',
  upstream_id: '',
  key_id: '',
  display_group: '',
  pool_enabled: true,
  manual_cost_ratio: '',
  enabled: true,
  public_enabled: false,
}

function cardToForm(card?: ModelCard): CardForm {
  if (!card) return emptyCardForm
  const custom = Boolean(card.base_url)
  return {
    name: card.name || '',
    source: custom ? 'custom' : 'upstream',
    base_url: card.base_url || '',
    api_key: card.api_key || '',
    model: card.model || 'gpt-5.6-sol',
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
    model: form.source === 'custom' ? form.model.trim() : '',
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
    return Boolean(form.base_url.trim() && form.model.trim() && (form.api_key.trim() || card?.api_key_set))
  }
  return Boolean(form.upstream_id && form.key_id)
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
  const sortCards = useMutation({
    mutationFn: (ids: string[]) => api('/api/cards/order', { method: 'POST', body: JSON.stringify({ ids }) }),
    onSuccess: async () => {
      await Promise.all([qc.invalidateQueries({ queryKey: ['status'] }), qc.invalidateQueries({ queryKey: ['cards'] })])
    },
    onError: alertError,
  })
  const onDragEnd = (event: DragEndEvent) => {
    setDraftCards((value) => reorderByDragEnd(value, event) ?? value)
  }
  const saveSort = () => {
    sortCards.mutate(draftCards.map((card) => card.id).filter(Boolean), {
      onSuccess: () => {
        setLayoutEditing(false)
      },
    })
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
                {sortCards.isPending ? '保存中...' : '保存排序'}
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
          <SortableGrid itemIds={shownCards.map((card) => card.id)} onDragEnd={onDragEnd}>
            {shownCards.map((card) => (
              <SortableItem key={card.id} id={card.id}>
                {({ attributes, listeners }) => (
                  <AdminStatusMonitorCard
                    card={card}
                    windowValue={windowValue}
                    rows={upstreams.data ?? []}
                    cards={cards}
                    dragHandle={<DragHandle sorting={sortCards.isPending} attributes={attributes} listeners={listeners} />}
                  />
                )}
              </SortableItem>
            ))}
          </SortableGrid>
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

function CardDialog({ rows, cards, card }: { rows: UpstreamRow[]; cards: ModelCard[]; card?: ModelCard }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<CardForm>(() => cardToForm(card))
  const keys = keysOf(rows.find((row) => row.upstream.id === form.upstream_id))
  const displayGroups = existingDisplayGroups(cards)
  const displayGroupListID = card ? `display-groups-${card.id}` : 'display-groups-new'
  const fb = useFeedback()
  const save = useMutation({
    mutationFn: () =>
      api(card ? `/api/cards/${card.id}` : '/api/cards', {
        method: card ? 'PATCH' : 'POST',
        body: JSON.stringify(cardPayload(form)),
      }),
    onMutate: () => fb.pending(),
    onSuccess: () => {
      fb.success()
      void invalidateMonitor(qc)
      closeAfterSave(setOpen)
    },
    onError: fb.fail,
  })
  const remove = useMutation({
    mutationFn: () => api(`/api/cards/${card?.id ?? ''}`, { method: 'DELETE' }),
    onSuccess: () => {
      setOpen(false)
      void invalidateMonitor(qc)
    },
    onError: alertError,
  })
  const update = (patch: Partial<CardForm>) => {
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
          setForm(cardToForm(card))
        }
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
                  placeholder={secretPlaceholder(card?.api_key_set)}
                  onChange={(e) => update({ api_key: e.target.value })}
                />
              </Field>
              <Field label="探测模型">
                <Input value={form.model} onChange={(e) => update({ model: e.target.value })} />
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
        </div>
        <FeedbackBanner message={fb.message} error={save.isError} />
        <ActionRow>
          {card && <IconAction title="删除" onClick={() => confirmDelete(card.name, '只删除卡片，历史探测记录会保留。') && remove.mutate()} pending={remove.isPending} icon={Trash2} danger />}
          <SaveButton onClick={() => save.mutate()} pending={save.isPending} disabled={!cardFormReady(form, card)} message={fb.message} />
        </ActionRow>
      </DialogContent>
    </Dialog>
  )
}

