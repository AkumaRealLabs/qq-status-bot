import { useState, type ReactNode } from 'react'
import { ChevronRight, Loader2, MonitorCheck } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { NavTab, TabID } from '@/types'

export function NavItem({ item, active, onClick }: { item: NavTab; active: boolean; onClick: () => void }) {
  return (
    <button
      className={cn(
        'flex h-9 items-center gap-2 rounded-md px-3 text-sm font-medium transition-colors',
        active ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'text-muted-foreground hover:bg-sidebar-accent hover:text-foreground',
      )}
      onClick={onClick}
    >
      <item.icon className="size-4" />
      <span className="flex-1 text-left">{item.label}</span>
      {active && <ChevronRight className="size-4 text-primary" />}
    </button>
  )
}

export function BrandIcon({ src, className }: { src?: string; className?: string }) {
  const [failed, setFailed] = useState(false)
  if (src && !failed) {
    return <img src={src} alt="" className={cn('size-9 rounded-lg object-cover', className)} onError={() => setFailed(true)} />
  }
  return (
    <div className={cn('flex size-9 items-center justify-center rounded-lg bg-surface-dark text-on-dark', className)}>
      <MonitorCheck className="size-5" />
    </div>
  )
}

export function MobileTabs({ tab, setTab, tabs }: { tab: TabID; setTab: (tab: TabID) => void; tabs: NavTab[] }) {
  return (
    <div className="mb-4 flex min-w-0 gap-2 overflow-x-auto pb-1 md:hidden">
      {tabs.map((item) => (
        <Button key={item.id} variant={tab === item.id ? 'secondary' : 'outline'} size="sm" className="min-w-20 px-3" onClick={() => setTab(item.id)}>
          <item.icon className="size-4" />
          {item.short}
        </Button>
      ))}
    </div>
  )
}

export function ShellLoading() {
  return (
    <div className="grid min-h-svh place-items-center bg-background">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        加载中
      </div>
    </div>
  )
}

export function Page({
  title,
  description,
  actions,
  children,
}: {
  title: string
  description?: string
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="grid min-w-0 animate-in fade-in-50 slide-in-from-bottom-1 gap-5 duration-300">
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h1 className="break-words font-display text-3xl font-normal leading-tight tracking-tight sm:text-4xl">{title}</h1>
          {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="min-w-0 sm:shrink-0">{actions}</div>}
      </div>
      {children}
    </section>
  )
}
