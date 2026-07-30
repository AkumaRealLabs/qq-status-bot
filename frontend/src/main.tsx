import { StrictMode, useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'

type Settings = {
  qqbot_app_id: string
  qqbot_app_secret?: string
  qqbot_app_secret_set?: boolean
  qqbot_allowed_groups: string[]
  status_commands: string[]
  status_url: string
  status_page_id: string
  status_period: string
  screenshot_timeout_seconds: number
  screenshot_queue_size: number
}

type Log = {
  id: string
  direction: string
  event_type: string
  group_openid?: string
  message_id?: string
  status: string
  message?: string
  created_at: string
}

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { credentials: 'same-origin', ...init, headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) } })
  const body = await response.json().catch(() => ({})) as { error?: string }
  if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`)
  return body as T
}

function App() {
  const [initialized, setInitialized] = useState<boolean | null>(null)
  const [authenticated, setAuthenticated] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    void api<{ initialized: boolean }>('/api/setup/status').then((result) => {
      setInitialized(result.initialized)
      if (result.initialized) void api('/api/auth/me').then(() => setAuthenticated(true)).catch(() => undefined)
    }).catch((reason: Error) => setError(reason.message))
  }, [])
  if (initialized === null) return <main className="center"><div className="panel"><p>正在连接服务...</p></div></main>
  if (!initialized) return <Auth title="初始化管理员" submit="创建并登录" onSuccess={() => { setInitialized(true); setAuthenticated(true) }} setError={setError} error={error} setup />
  if (!authenticated) return <Auth title="管理员登录" submit="登录" onSuccess={() => setAuthenticated(true)} setError={setError} error={error} />
  return <Dashboard onLogout={() => { void api('/api/auth/logout', { method: 'POST' }).finally(() => setAuthenticated(false)) }} />
}

function Auth({ title, submit, setup = false, onSuccess, setError, error }: { title: string; submit: string; setup?: boolean; onSuccess: () => void; setError: (value: string) => void; error: string }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault(); setBusy(true); setError('')
    try { await api(setup ? '/api/setup' : '/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }); onSuccess() } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败') } finally { setBusy(false) }
  }
  return <main className="center"><form className="panel auth" onSubmit={(event) => void handleSubmit(event)}><div className="brand"><span className="brand-mark">Q</span><div><strong>QQ 状态机器人</strong><small>官方 QQ 开放平台</small></div></div><h1>{title}</h1><label>管理员账号<input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" /></label><label>管理员密码<input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete={setup ? 'new-password' : 'current-password'} /></label>{error && <p className="error">{error}</p>}<button disabled={busy || !password}>{busy ? '处理中...' : submit}</button></form></main>
}

function Dashboard({ onLogout }: { onLogout: () => void }) {
  const [tab, setTab] = useState<'settings' | 'logs'>('settings')
  const [settings, setSettings] = useState<Settings | null>(null)
  const [logs, setLogs] = useState<Log[]>([])
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
	const [previewURL, setPreviewURL] = useState('')
	const [previewBusy, setPreviewBusy] = useState(false)
  const load = async () => { try { setSettings(await api<Settings>('/api/settings')); setLogs(await api<Log[]>('/api/logs?limit=100')) } catch (reason) { setError(reason instanceof Error ? reason.message : '加载失败') } }
  useEffect(() => { void load() }, [])
  const status = useMemo(() => logs.filter((item) => item.status === 'failed').length, [logs])
  async function save() { if (!settings) return; setMessage(''); setError(''); try { setSettings(await api<Settings>('/api/settings', { method: 'PATCH', body: JSON.stringify(settings) })); setMessage('配置已保存') } catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败') } }
	async function preview() {
		if (!settings) return
		setPreviewBusy(true); setMessage(''); setError('')
		try {
			const updated = await api<Settings>('/api/settings', { method: 'PATCH', body: JSON.stringify(settings) })
			setSettings(updated)
			const response = await fetch('/api/status-preview', { credentials: 'same-origin', cache: 'no-store' })
			if (!response.ok) {
				const body = await response.json().catch(() => ({})) as { error?: string }
				throw new Error(body.error ?? `HTTP ${response.status}`)
			}
			const objectURL = URL.createObjectURL(await response.blob())
			setPreviewURL((current) => { if (current) URL.revokeObjectURL(current); return objectURL })
			setMessage('状态图已生成')
		} catch (reason) {
			setError(reason instanceof Error ? reason.message : '预览失败')
		} finally {
			setPreviewBusy(false)
		}
	}
	return <main className="shell"><header><div className="brand"><span className="brand-mark">Q</span><div><strong>QQ 状态机器人</strong><small>官方机器人控制台</small></div></div><div className="header-actions"><span className={status ? 'health bad' : 'health'}>{status ? `${status} 条失败` : '运行正常'}</span><button className="ghost" onClick={onLogout}>退出</button></div></header><nav className="tabs"><button className={tab === 'settings' ? 'active' : ''} onClick={() => setTab('settings')}>机器人配置</button><button className={tab === 'logs' ? 'active' : ''} onClick={() => { setTab('logs'); void load() }}>收发日志</button></nav>{error && <p className="error banner">{error}</p>}{message && <p className="success banner">{message}</p>}{tab === 'settings' && settings && <SettingsPanel value={settings} setValue={setSettings} save={() => void save()} preview={() => void preview()} previewBusy={previewBusy} previewURL={previewURL} />}{tab === 'logs' && <Logs logs={logs} refresh={() => void load()} />}</main>
}

function SettingsPanel({ value, setValue, save, preview, previewBusy, previewURL }: { value: Settings; setValue: (value: Settings) => void; save: () => void; preview: () => void; previewBusy: boolean; previewURL: string }) {
  const set = <K extends keyof Settings>(key: K, next: Settings[K]) => setValue({ ...value, [key]: next })
  return <section className="content"><div className="title-row"><div><h1>机器人配置</h1><p>配置官方凭证、状态图数据源和群命令，保存后立即生效。</p></div><div className="title-actions"><button className="secondary" disabled={previewBusy} onClick={preview}>{previewBusy ? '生成中...' : '生成预览'}</button><button onClick={save}>保存配置</button></div></div><div className="grid"><article className="card"><h2>QQ 开放平台</h2><p className="muted">回调地址：当前域名 /qqbot/events</p><label>AppID<input value={value.qqbot_app_id} onChange={(event) => set('qqbot_app_id', event.target.value)} /></label><label>AppSecret<input type="password" placeholder={value.qqbot_app_secret_set ? '已保存，留空表示不修改' : '请输入 AppSecret'} value={value.qqbot_app_secret ?? ''} onChange={(event) => set('qqbot_app_secret', event.target.value)} /></label><label>允许的群 OpenID< textarea value={value.qqbot_allowed_groups.join('\n')} onChange={(event) => set('qqbot_allowed_groups', event.target.value.split(/\n|,/))} placeholder="留空表示允许全部群，每行一个" /></label><label>状态命令<input value={value.status_commands.join(',')} onChange={(event) => set('status_commands', event.target.value.split(','))} /></label></article><article className="card"><h2>状态图数据源</h2><label>URL<input value={value.status_url} onChange={(event) => set('status_url', event.target.value)} /></label><div className="row"><label>Page ID<input value={value.status_page_id} onChange={(event) => set('status_page_id', event.target.value)} /></label><label>统计周期<select value={value.status_period} onChange={(event) => set('status_period', event.target.value)}><option value="24h">近 24 小时</option><option value="7d">近 7 天</option><option value="30d">近 30 天</option><option value="90d">近 90 天</option><option value="1y">近 1 年</option></select></label></div><div className="row"><label>总超时（秒）<input type="number" min="15" max="240" value={value.screenshot_timeout_seconds} onChange={(event) => set('screenshot_timeout_seconds', Number(event.target.value))} /></label><label>队列长度<input type="number" min="1" max="20" value={value.screenshot_queue_size} onChange={(event) => set('screenshot_queue_size', Number(event.target.value))} /></label></div></article>{previewURL && <article className="card preview-card"><h2>状态图预览</h2><img src={previewURL} alt="状态图预览" /></article>}</div></section>
}

function Logs({ logs, refresh }: { logs: Log[]; refresh: () => void }) {
  return <section className="content"><div className="title-row"><div><h1>收发日志</h1><p>仅记录事件摘要，不保存完整请求体、签名或 Access Token。</p></div><button className="secondary" onClick={refresh}>刷新</button></div><div className="log-list">{logs.length === 0 ? <div className="empty">暂无收发日志</div> : logs.map((item) => <div className="log" key={item.id}><div className="log-top"><span className={item.direction === 'send' ? 'tag send' : 'tag'}>{item.direction === 'send' ? '发送' : '接收'}</span><strong>{item.event_type}</strong><span className={`state ${item.status}`}>{item.status}</span><time>{new Date(item.created_at).toLocaleString()}</time></div><div className="log-detail">群：{item.group_openid || '-'}　消息：{item.message_id || '-'}　{item.message || ''}</div></div>)}</div></section>
}

createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>)
