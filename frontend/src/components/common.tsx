import { useState, type CSSProperties, type ElementType, type ReactNode } from 'react'
import { CheckCircle2, Loader2, Save, XCircle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { feedbackTone } from '@/lib/feedback'
import { errorMessage } from '@/lib/format'
import { cn } from '@/lib/utils'

const windows = ['1h', '3h', '5h', '1d', '7d', '15d']

export function InlineMessage({
  message,
  tone = 'neutral',
  className,
}: {
  message: string
  tone?: 'neutral' | 'success' | 'error' | 'warning'
  className?: string
}) {
  if (!message) return null
  return (
    <div
      className={cn(
        'min-w-0 animate-in break-words rounded-md border px-3 py-2 text-sm fade-in slide-in-from-top-1',
        tone === 'error' && 'border-destructive/30 bg-destructive/10 text-destructive',
        tone === 'success' && 'border-success/30 bg-success/10 text-success',
        tone === 'warning' && 'border-warning/30 bg-warning/10 text-warning',
        tone === 'neutral' && 'border-border bg-secondary text-muted-foreground',
        className,
      )}
    >
      {message}
    </div>
  )
}

export function FormError({ error }: { error: unknown }) {
  if (!error) return null
  return <InlineMessage message={errorMessage(error)} tone="error" />
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-2">
      <label className="text-sm font-medium leading-none text-foreground">{label}</label>
      {children}
    </div>
  )
}

export function Metric({ label, value, accent }: { label: string; value: ReactNode; accent?: 'success' | 'danger' }) {
  return (
    <Card className="gap-1.5 bg-card py-3">
      <CardContent className="px-4">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className={cn('font-display mt-0.5 break-words text-2xl font-normal', accent === 'success' && 'text-success', accent === 'danger' && 'text-destructive')}>
          {value}
        </div>
      </CardContent>
    </Card>
  )
}

export function MiniStat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="min-w-0 rounded-md border border-border bg-background px-2.5 py-1.5">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate text-sm font-medium">{value}</div>
    </div>
  )
}

export function EmptyPanel({ text }: { text: string }) {
  return (
    <Card className="bg-card">
      <CardContent className="py-12 text-center text-muted-foreground">{text}</CardContent>
    </Card>
  )
}

export function SkeletonCardGrid({ count }: { count: number }) {
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

export function HoverText({
  value,
  className,
  children,
  content,
  nativeTitle = false,
  alwaysTooltip = false,
}: {
  value?: string
  className?: string
  children?: ReactNode
  content?: ReactNode
  nativeTitle?: boolean
  alwaysTooltip?: boolean
}) {
  const text = value || '-'
  const [tooltipStyle, setTooltipStyle] = useState<CSSProperties>()
  const [showTooltip, setShowTooltip] = useState(false)
  const placeTooltip = (target: HTMLElement) => {
    const textNode = target.querySelector<HTMLElement>('[data-hover-text]')
    const truncated = textNode ? textNode.scrollWidth > textNode.clientWidth : false
    setShowTooltip(Boolean(alwaysTooltip || content || children || truncated))
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
      onMouseLeave={() => setShowTooltip(false)}
      onBlur={() => setShowTooltip(false)}
    >
      {children ?? <span data-hover-text className="block truncate">{text}</span>}
      {text !== '-' && showTooltip && (
        <span
          className="pointer-events-none fixed z-50 w-max min-w-56 max-w-[min(520px,calc(100vw-32px))] rounded-md border border-border bg-popover text-left text-popover-foreground shadow-[0_1px_3px_rgba(20,20,19,0.08)]"
          style={tooltipStyle}
        >
          {content ?? <span className="block whitespace-pre-wrap break-words px-3 py-2 text-sm leading-[1.55]">{text}</span>}
        </span>
      )}
    </span>
  )
}

export function TypeBadge({ type }: { type: string }) {
  return <Badge variant="secondary">{type === 'newapi' ? 'new-api' : 'sub2api'}</Badge>
}

export function StatusBadge({ ok, okText, failText }: { ok: boolean; okText: string; failText: string }) {
  return (
    <Badge variant={ok ? 'success' : 'destructive'}>
      {ok ? <CheckCircle2 className="size-3" /> : <XCircle className="size-3" />}
      {ok ? okText : failText}
    </Badge>
  )
}

export function WindowSelect({ value, setValue }: { value: string; setValue: (value: string) => void }) {
  return (
    <div className="max-w-full overflow-x-auto rounded-md border border-border bg-background">
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

export function IconAction({
  title,
  icon: Icon,
  onClick,
  pending,
  danger,
  disabled,
}: {
  title: string
  icon: ElementType
  onClick: () => void
  pending: boolean
  danger?: boolean
  disabled?: boolean
}) {
  return (
    <Button variant={danger ? 'danger' : 'outline'} size="icon" onClick={onClick} disabled={pending || disabled} title={title}>
      {pending ? <Loader2 className="size-4 animate-spin" /> : <Icon className="size-4" />}
      <span className="sr-only">{title}</span>
    </Button>
  )
}

/** 对话框/表单底部行：右侧操作按钮。 */
export function ActionRow({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('flex flex-wrap items-center justify-end gap-2', className)}>{children}</div>
}

/**
 * 主保存按钮：pending / success 文案切换。
 * 传入 `message === successLabel`（默认 已保存）时在成功后切换标签。
 */
export function SaveButton({
  onClick,
  pending,
  disabled,
  message = '',
  label = '保存',
  pendingLabel = '保存中...',
  successLabel = '已保存',
  icon: Icon = Save,
  className,
}: {
  onClick: () => void
  pending: boolean
  disabled?: boolean
  message?: string
  label?: string
  pendingLabel?: string
  successLabel?: string
  icon?: ElementType
  className?: string
}) {
  const done = !pending && message === successLabel
  return (
    <Button onClick={onClick} disabled={pending || disabled} className={className}>
      {pending ? <Loader2 className="size-4 animate-spin" /> : <Icon className="size-4" />}
      {pending ? pendingLabel : done ? successLabel : label}
    </Button>
  )
}

/** InlineMessage + 通用语气推导，用于操作反馈。 */
export function FeedbackBanner({
  message,
  error,
  success = '已保存',
  className,
}: {
  message: string
  error?: boolean
  success?: string | string[]
  className?: string
}) {
  return <InlineMessage message={message} tone={feedbackTone(message, { error, success })} className={className} />
}

/** 带边框可滚动表格壳，运维与收入页面共用。 */
export function DataTable({
  minWidthClass,
  head,
  children,
  className,
}: {
  minWidthClass?: string
  head: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cn('overflow-x-auto rounded-md border border-border', className)}>
      <table className={cn('w-full text-left text-sm', minWidthClass)}>
        <thead className="bg-secondary text-xs text-muted-foreground">{head}</thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  )
}
