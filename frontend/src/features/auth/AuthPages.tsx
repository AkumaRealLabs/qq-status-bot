import { useState, type ReactNode } from 'react'
import { useMutation } from '@tanstack/react-query'
import { ExternalLink, Loader2 } from 'lucide-react'
import { FormError, Field } from '@/components/common'
import { BrandIcon } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import type { SiteSettings } from '@/types'

export function SetupPage({ site, onDone }: { site?: SiteSettings; onDone: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const mutation = useMutation({
    mutationFn: () => api('/api/setup', { method: 'POST', body: JSON.stringify({ username, password }) }),
    onSuccess: onDone,
  })
  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (!mutation.isPending) mutation.mutate()
  }
  return (
    <AuthFrame site={site} title="初始化管理员" subtitle="创建第一个管理员账号">
      <form className="grid gap-4" autoComplete="off" onSubmit={submit}>
        <FormError error={mutation.error} />
        <Field label="用户名">
          <Input name="monitor-setup-user" autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="密码">
          <Input name="monitor-setup-pass" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending && <Loader2 className="size-4 animate-spin" />}
          创建管理员
        </Button>
      </form>
    </AuthFrame>
  )
}

export function LoginPage({ site, onDone }: { site?: SiteSettings; onDone: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const mutation = useMutation({
    mutationFn: () => api('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
    onSuccess: onDone,
  })
  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (!mutation.isPending) mutation.mutate()
  }
  return (
    <AuthFrame site={site} title="登录" subtitle={`进入 ${site?.site_name || 'AI 上游监控'} 后台`}>
      <form className="grid gap-4" autoComplete="off" onSubmit={submit}>
        <FormError error={mutation.error} />
        <Field label="用户名">
          <Input name="monitor-login-user" autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="密码">
          <Input name="monitor-login-pass" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending && <Loader2 className="size-4 animate-spin" />}
          登录
        </Button>
      </form>
    </AuthFrame>
  )
}

export function AuthFrame({ site, title, subtitle, children }: { site?: SiteSettings; title: string; subtitle: string; children: ReactNode }) {
  return (
    <div className="grid min-h-svh place-items-center bg-background p-4">
      <Card className="animate-in w-full max-w-sm bg-card fade-in-50 zoom-in-95 duration-300">
        <CardHeader className="gap-2 text-center">
          <BrandIcon src={site?.site_icon} className="mx-auto" />
          <CardTitle className="font-display text-3xl font-normal">{title}</CardTitle>
          <CardDescription>{subtitle}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {children}
          <Button asChild variant="outline">
            <a href="/">
              <ExternalLink className="size-4" />
              前台
            </a>
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
