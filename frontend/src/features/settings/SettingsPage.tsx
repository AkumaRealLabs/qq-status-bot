import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, Loader2, Upload } from 'lucide-react'
import { Field, FeedbackBanner, SaveButton } from '@/components/common'
import { Page, ShellLoading } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { secretPlaceholder, useFeedback } from '@/lib/feedback'
import { invalidateMonitor } from '@/lib/query'
import type { SettingsData } from '@/types'

const buildVersion = import.meta.env.VITE_BUILD_VERSION || 'dev'

export function SettingsPage() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['settings'], queryFn: () => api<SettingsData>('/api/settings') })
  const [form, setForm] = useState<SettingsData | null>(null)
  const saveFb = useFeedback('已保存')
  const backupFb = useFeedback('已导出|导入完成')
  const fileInputRef = useRef<HTMLInputElement>(null)
  const data = form ?? q.data
  const save = useMutation({
    mutationFn: () => api('/api/settings', { method: 'PATCH', body: JSON.stringify(data) }),
    onMutate: () => saveFb.pending(),
    onSuccess: () => {
      saveFb.success()
      setForm(null)
      void qc.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: saveFb.fail,
  })
  const importData = useMutation({
    mutationFn: (text: string) => api('/api/settings/import', { method: 'POST', body: text }),
    onMutate: () => backupFb.pending('导入中...'),
    onSuccess: () => {
      backupFb.success('导入完成')
      setForm(null)
      void invalidateMonitor(qc)
      void qc.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: backupFb.fail,
  })
  async function exportData() {
    backupFb.pending('导出中...')
    try {
      const res = await fetch('/api/settings/export', { credentials: 'include' })
      if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || `HTTP ${res.status}`)
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `ai-upstream-monitor-sensitive-${new Date().toISOString().slice(0, 10)}.json`
      a.click()
      URL.revokeObjectURL(url)
      backupFb.success('已导出')
    } catch (error) {
      backupFb.fail(error)
    }
  }
  async function onImportFile(file?: File) {
    if (!file) return
    if (!window.confirm('导入会替换当前业务数据，继续吗？')) return
    importData.mutate(await file.text())
    if (fileInputRef.current) fileInputRef.current.value = ''
  }
  if (!data) return <ShellLoading />
  return (
    <Page title="设置" description="监控周期、Telegram 告警和数据备份">
      <Card className="w-full max-w-2xl bg-card">
        <CardHeader>
          <CardTitle>基础设置</CardTitle>
          <CardDescription>探测模型由后端固定为 gpt-5.5</CardDescription>
        </CardHeader>
        <CardContent className="grid min-w-0 gap-4">
          <Field label="站点名称">
            <Input value={data.site_name ?? ''} onChange={(e) => setForm({ ...data, site_name: e.target.value })} />
          </Field>
          <Field label="站点图标 URL">
            <Input value={data.site_icon ?? ''} placeholder="/favicon.ico 或 https://..." onChange={(e) => setForm({ ...data, site_icon: e.target.value })} />
          </Field>
          <Field label="检查间隔（分钟）">
            <Input type="number" value={data.check_interval_minutes} onChange={(e) => setForm({ ...data, check_interval_minutes: Number(e.target.value) })} />
          </Field>
          <Field label="Telegram Bot Token">
            <Input
              type="password"
              value={data.telegram_bot_token ?? ''}
              placeholder={secretPlaceholder(data.telegram_bot_token_set)}
              onChange={(e) => setForm({ ...data, telegram_bot_token: e.target.value })}
            />
          </Field>
          <Field label="Telegram Chat ID">
            <Input value={data.telegram_chat_id ?? ''} onChange={(e) => setForm({ ...data, telegram_chat_id: e.target.value })} />
          </Field>
          <Field label="探测模型">
            <Input value={data.probe_model} disabled />
          </Field>
          <Field label="构建版本">
            <Input value={buildVersion} disabled />
          </Field>
        </CardContent>
      </Card>
      <div className="grid w-full max-w-2xl gap-3">
        <FeedbackBanner message={saveFb.message} error={save.isError} />
        <div>
          <SaveButton onClick={() => save.mutate()} pending={save.isPending} message={saveFb.message} label="保存设置" />
        </div>
      </div>
      <Card className="w-full max-w-2xl bg-card">
        <CardHeader>
          <CardTitle>数据备份</CardTitle>
          <CardDescription>导出文件包含密钥、Token 和 Telegram 会话，请按敏感备份保存</CardDescription>
        </CardHeader>
        <CardContent className="grid min-w-0 gap-4">
          <FeedbackBanner
            message={backupFb.message}
            error={importData.isError || isBackupError(backupFb.message)}
            success="已导出|导入完成"
          />
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => void exportData()}>
              <Download className="size-4" />
              导出敏感备份
            </Button>
            <Button variant="outline" onClick={() => fileInputRef.current?.click()} disabled={importData.isPending}>
              {importData.isPending ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />}
              导入 JSON
            </Button>
            <input ref={fileInputRef} className="hidden" type="file" accept="application/json,.json" onChange={(e) => void onImportFile(e.target.files?.[0])} />
          </div>
        </CardContent>
      </Card>
    </Page>
  )
}

function isBackupError(message: string) {
  if (!message) return false
  return !['已导出', '导入完成', '导出中...', '导入中...'].includes(message)
}
