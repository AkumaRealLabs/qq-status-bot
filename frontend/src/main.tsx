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
  alerts_enabled: boolean
  alert_groups: string[]
  alert_failure_samples: number
  alert_recovery_samples: number
  ggapi_balance_enabled: boolean
  ggapi_base_url: string
  ggapi_admin_token?: string
  ggapi_admin_token_set?: boolean
  ggapi_smtp_host: string
  ggapi_smtp_port: number
  ggapi_smtp_username: string
  ggapi_smtp_password?: string
  ggapi_smtp_password_set?: boolean
  ggapi_smtp_from: string
  ggapi_smtp_from_name: string
  ggapi_smtp_tls_mode: string
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

type AccountBinding = {
  id: string
  email: string
  ggapi_user_id: string
  username: string
  first_group_openid: string
  bound_at: string
}

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { credentials: 'same-origin', ...init, headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) } })
  const body = await response.json().catch(() => ({})) as { error?: string }
  if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`)
  return body as T
}

function normalizeSettings(value: Settings): Settings {
  return {
    ...value,
    qqbot_allowed_groups: value.qqbot_allowed_groups ?? [],
    status_commands: value.status_commands ?? [],
    alert_groups: value.alert_groups ?? [],
    alert_failure_samples: value.alert_failure_samples || 2,
    alert_recovery_samples: value.alert_recovery_samples || 2,
    alerts_enabled: value.alerts_enabled ?? false,
    ggapi_balance_enabled: value.ggapi_balance_enabled ?? false,
    ggapi_base_url: value.ggapi_base_url ?? 'https://www.ggapi.cc',
    ggapi_smtp_host: value.ggapi_smtp_host ?? '',
    ggapi_smtp_port: value.ggapi_smtp_port || 587,
    ggapi_smtp_username: value.ggapi_smtp_username ?? '',
    ggapi_smtp_from: value.ggapi_smtp_from ?? '',
    ggapi_smtp_from_name: value.ggapi_smtp_from_name ?? '',
    ggapi_smtp_tls_mode: value.ggapi_smtp_tls_mode ?? 'starttls',
  }
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
  if (!initialized) return <Auth title="设置管理密码" submit="创建并登录" onSuccess={() => { setInitialized(true); setAuthenticated(true) }} setError={setError} error={error} setup />
  if (!authenticated) return <Auth title="管理员登录" submit="登录" onSuccess={() => setAuthenticated(true)} setError={setError} error={error} />
  return <Dashboard onLogout={() => { void api('/api/auth/logout', { method: 'POST' }).finally(() => setAuthenticated(false)) }} />
}

function Auth({ title, submit, setup = false, onSuccess, setError, error }: { title: string; submit: string; setup?: boolean; onSuccess: () => void; setError: (value: string) => void; error: string }) {
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault(); setBusy(true); setError('')
    try { await api(setup ? '/api/setup' : '/api/auth/login', { method: 'POST', body: JSON.stringify({ password }) }); onSuccess() } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败') } finally { setBusy(false) }
  }
  return <main className="center"><form className="panel auth" onSubmit={(event) => void handleSubmit(event)}><div className="brand"><span className="brand-mark">Q</span><div><strong>QQ 状态机器人</strong><small>官方 QQ 开放平台</small></div></div><h1>{title}</h1><label>管理密码<input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete={setup ? 'new-password' : 'current-password'} /></label>{error && <p className="error">{error}</p>}<button disabled={busy || !password}>{busy ? '处理中...' : submit}</button></form></main>
}

function Dashboard({ onLogout }: { onLogout: () => void }) {
  const [tab, setTab] = useState<'settings' | 'logs' | 'bindings'>('settings')
  const [settings, setSettings] = useState<Settings | null>(null)
  const [logs, setLogs] = useState<Log[]>([])
  const [bindings, setBindings] = useState<AccountBinding[]>([])
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
	const [previewURL, setPreviewURL] = useState('')
	const [previewBusy, setPreviewBusy] = useState(false)
  const load = async () => {
    try {
      setSettings(normalizeSettings(await api<Settings>('/api/settings')))
      setLogs((await api<Log[] | null>('/api/logs?limit=100')) ?? [])
      setBindings((await api<AccountBinding[] | null>('/api/account-bindings')) ?? [])
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '加载失败')
    }
  }
  useEffect(() => { void load() }, [])
  const status = useMemo(() => logs.filter((item) => item.status === 'failed').length, [logs])
  async function save() { if (!settings) return; setMessage(''); setError(''); try { setSettings(normalizeSettings(await api<Settings>('/api/settings', { method: 'PATCH', body: JSON.stringify(settings) }))); setMessage('配置已保存') } catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败') } }
	async function preview() {
		if (!settings) return
		setPreviewBusy(true); setMessage(''); setError('')
		try {
			const updated = await api<Settings>('/api/settings', { method: 'PATCH', body: JSON.stringify(settings) })
			setSettings(normalizeSettings(updated))
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
	return <main className="shell"><header><div className="brand"><span className="brand-mark">Q</span><div><strong>QQ 状态机器人</strong><small>官方机器人控制台</small></div></div><div className="header-actions"><span className={status ? 'health bad' : 'health'}>{status ? `${status} 条失败` : '运行正常'}</span><button className="ghost" onClick={onLogout}>退出</button></div></header><nav className="tabs"><button className={tab === 'settings' ? 'active' : ''} onClick={() => setTab('settings')}>机器人配置</button><button className={tab === 'bindings' ? 'active' : ''} onClick={() => { setTab('bindings'); void load() }}>账号绑定</button><button className={tab === 'logs' ? 'active' : ''} onClick={() => { setTab('logs'); void load() }}>收发日志</button></nav>{error && <p className="error banner">{error}</p>}{message && <p className="success banner">{message}</p>}{tab === 'settings' && settings && <SettingsPanel value={settings} setValue={setSettings} save={() => void save()} preview={() => void preview()} previewBusy={previewBusy} previewURL={previewURL} />}{tab === 'bindings' && <Bindings items={bindings} refresh={() => void load()} onDeleted={(id) => setBindings((current) => current.filter((item) => item.id !== id))} />}{tab === 'logs' && <Logs logs={logs} refresh={() => void load()} />}</main>
}

function SettingsPanel({ value, setValue, save, preview, previewBusy, previewURL }: { value: Settings; setValue: (value: Settings) => void; save: () => void; preview: () => void; previewBusy: boolean; previewURL: string }) {
	const [testBusy, setTestBusy] = useState('')
	const [testMessage, setTestMessage] = useState<Record<string, string>>({})
	const [smtpTestRecipient, setSmtpTestRecipient] = useState('')
	const [smtpTestBusy, setSmtpTestBusy] = useState(false)
	const [smtpTestMessage, setSmtpTestMessage] = useState<{ ok: boolean; text: string } | null>(null)
	const [discoveredGroups, setDiscoveredGroups] = useState<string[]>([])
	const [selectedGroup, setSelectedGroup] = useState('')
	const [selectedActionGroup, setSelectedActionGroup] = useState('')
	const [actionBusy, setActionBusy] = useState('')
	const [actionResult, setActionResult] = useState<{ ok: boolean; message: string } | null>(null)
	const set = <K extends keyof Settings>(key: K, next: Settings[K]) => setValue({ ...value, [key]: next })
	const availableGroups = discoveredGroups.filter((group) => !value.alert_groups.some((configured) => configured.trim() === group))
	const selectableGroup = availableGroups.includes(selectedGroup) ? selectedGroup : ''
	const actionGroups = [...new Set([...discoveredGroups, ...value.alert_groups, ...value.qqbot_allowed_groups].map((group) => group.trim()).filter(Boolean))]
	const actionGroup = actionGroups.includes(selectedActionGroup) ? selectedActionGroup : (actionGroups[0] ?? '')
	async function loadDiscoveredGroups() {
		try {
			const groups = (await api<string[] | null>('/api/groups/discovered')) ?? []
			setDiscoveredGroups(groups)
			setSelectedGroup((current) => groups.includes(current) ? current : (groups.find((group) => !value.alert_groups.some((configured) => configured.trim() === group)) ?? ''))
		} catch { setDiscoveredGroups([]); setSelectedGroup('') }
	}
	useEffect(() => { void loadDiscoveredGroups() }, [])
	function addDiscoveredGroup() {
		if (!selectableGroup) return
		set('alert_groups', [...value.alert_groups.filter((group) => group.trim() !== ''), selectableGroup])
		setSelectedGroup(availableGroups.find((group) => group !== selectableGroup) ?? '')
	}
	async function testGroup(group: string) {
		setTestBusy(group); setTestMessage((current) => ({ ...current, [group]: '' }))
		try {
			await api('/api/alerts/test', { method: 'POST', body: JSON.stringify({ group_openid: group }) })
			setTestMessage((current) => ({ ...current, [group]: '发送成功' }))
		} catch (reason) {
			setTestMessage((current) => ({ ...current, [group]: reason instanceof Error ? reason.message : '发送失败' }))
		} finally { setTestBusy('') }
	}
	async function testSMTP() {
		const recipient = smtpTestRecipient.trim()
		if (!recipient) {
			setSmtpTestMessage({ ok: false, text: '请填写测试收件邮箱' })
			return
		}
		setSmtpTestBusy(true); setSmtpTestMessage(null)
		try {
			const updated = await api<Settings>('/api/settings', { method: 'PATCH', body: JSON.stringify(value) })
			setValue(normalizeSettings(updated))
			await api('/api/ggapi/smtp-test', { method: 'POST', body: JSON.stringify({ recipient }) })
			setSmtpTestMessage({ ok: true, text: `测试邮件已发送至 ${recipient}` })
		} catch (reason) {
			setSmtpTestMessage({ ok: false, text: reason instanceof Error ? reason.message : '测试邮件发送失败' })
		} finally { setSmtpTestBusy(false) }
	}
	async function runAction(action: 'status' | 'offline' | 'recovery') {
		if (!actionGroup) return
		setActionBusy(action)
		setActionResult(null)
		try {
			const updated = await api<Settings>('/api/settings', { method: 'PATCH', body: JSON.stringify(value) })
			setValue(normalizeSettings(updated))
			const path = action === 'status' ? '/api/status/send' : '/api/alerts/simulate'
			const body = action === 'status' ? { group_openid: actionGroup } : { group_openid: actionGroup, kind: action }
			await api(path, { method: 'POST', body: JSON.stringify(body) })
			const labels = { status: '状态图已发送', offline: '模拟故障已发送', recovery: '模拟恢复已发送' }
			setActionResult({ ok: true, message: labels[action] })
		} catch (reason) {
			setActionResult({ ok: false, message: reason instanceof Error ? reason.message : '发送失败' })
		} finally {
			setActionBusy('')
		}
	}
	return <section className="content">
		<div className="title-row">
			<div><h1>机器人配置</h1><p>配置官方凭证、状态图数据源和群命令，保存后立即生效。</p></div>
			<div className="title-actions"><button className="secondary" disabled={previewBusy} onClick={preview}>{previewBusy ? '生成中...' : '生成预览'}</button><button onClick={save}>保存配置</button></div>
		</div>
		<div className="grid">
			<article className="card"><h2>QQ 开放平台</h2><p className="muted">回调地址：当前域名 /qqbot/events</p><label>AppID<input value={value.qqbot_app_id} onChange={(event) => set('qqbot_app_id', event.target.value)} /></label><label>AppSecret<input type="password" placeholder={value.qqbot_app_secret_set ? '已保存，留空表示不修改' : '请输入 AppSecret'} value={value.qqbot_app_secret ?? ''} onChange={(event) => set('qqbot_app_secret', event.target.value)} /></label><label>允许的群 OpenID<textarea value={value.qqbot_allowed_groups.join('\n')} onChange={(event) => set('qqbot_allowed_groups', event.target.value.split(/\n|,/))} placeholder="留空表示允许全部群，每行一个" /></label><label>状态命令<input value={value.status_commands.join(',')} onChange={(event) => set('status_commands', event.target.value.split(','))} /></label></article>
			<article className="card"><h2>状态图数据源</h2><label>URL<input value={value.status_url} onChange={(event) => set('status_url', event.target.value)} /></label><div className="row"><label>Page ID<input value={value.status_page_id} onChange={(event) => set('status_page_id', event.target.value)} /></label><label>统计周期<select value={value.status_period} onChange={(event) => set('status_period', event.target.value)}><option value="24h">近 24 小时</option><option value="7d">近 7 天</option><option value="30d">近 30 天</option><option value="90d">近 90 天</option><option value="1y">近 1 年</option></select></label></div><div className="row"><label>总超时（秒）<input type="number" min="15" max="240" value={value.screenshot_timeout_seconds} onChange={(event) => set('screenshot_timeout_seconds', Number(event.target.value))} /></label><label>队列长度<input type="number" min="1" max="20" value={value.screenshot_queue_size} onChange={(event) => set('screenshot_queue_size', Number(event.target.value))} /></label></div></article>
				<article className="card ggapi-card"><div className="section-heading"><div><h2>GGAPI 余额</h2><p className="muted">仅查询 GGAPI 钱包余额，不修改 GGAPI 账号。</p></div><label className="switch"><input type="checkbox" checked={value.ggapi_balance_enabled} onChange={(event) => set('ggapi_balance_enabled', event.target.checked)} /><span>启用</span></label></div><label>GGAPI HTTPS 地址<input value={value.ggapi_base_url} onChange={(event) => set('ggapi_base_url', event.target.value)} placeholder="https://www.ggapi.cc" /></label><label>管理令牌<input type="password" placeholder={value.ggapi_admin_token_set ? '已保存，留空表示不修改' : '请输入只读管理令牌'} value={value.ggapi_admin_token ?? ''} onChange={(event) => set('ggapi_admin_token', event.target.value)} /></label><div className="row"><label>SMTP 主机<input value={value.ggapi_smtp_host} onChange={(event) => set('ggapi_smtp_host', event.target.value)} /></label><label>SMTP 端口<input type="number" min="1" max="65535" value={value.ggapi_smtp_port} onChange={(event) => set('ggapi_smtp_port', Number(event.target.value))} /></label></div><div className="row"><label>SMTP 用户名<input value={value.ggapi_smtp_username} onChange={(event) => set('ggapi_smtp_username', event.target.value)} /></label><label>SMTP TLS 模式<select value={value.ggapi_smtp_tls_mode} onChange={(event) => set('ggapi_smtp_tls_mode', event.target.value)}><option value="starttls">STARTTLS</option><option value="implicit_tls">隐式 TLS</option></select></label></div><div className="row"><label>SMTP 密码<input type="password" placeholder={value.ggapi_smtp_password_set ? '已保存，留空表示不修改' : '请输入 SMTP 密码'} value={value.ggapi_smtp_password ?? ''} onChange={(event) => set('ggapi_smtp_password', event.target.value)} /></label><label>发件人地址<input value={value.ggapi_smtp_from} onChange={(event) => set('ggapi_smtp_from', event.target.value)} placeholder="noreply@example.com" /></label></div><label>发件人名称<input value={value.ggapi_smtp_from_name} onChange={(event) => set('ggapi_smtp_from_name', event.target.value)} /></label><div className="smtp-test"><label>测试收件邮箱<input type="email" value={smtpTestRecipient} onChange={(event) => setSmtpTestRecipient(event.target.value)} placeholder="name@example.com" /></label><button type="button" className="secondary" disabled={smtpTestBusy} onClick={() => void testSMTP()}>{smtpTestBusy ? '发送中...' : '发送测试邮件'}</button></div>{smtpTestMessage && <p className={smtpTestMessage.ok ? 'test-ok' : 'test-error'}>{smtpTestMessage.text}</p>}</article>
			<article className="card alert-card"><div className="section-heading"><div><h2>故障通知</h2><p className="muted">独立告警群接收状态故障与恢复通知。</p></div><label className="switch"><input type="checkbox" checked={value.alerts_enabled} onChange={(event) => set('alerts_enabled', event.target.checked)} /><span>启用</span></label></div><label>从已发现群添加<div className="discovered-picker"><select value={selectableGroup} onChange={(event) => setSelectedGroup(event.target.value)} disabled={availableGroups.length === 0}><option value="">{availableGroups.length === 0 ? '暂无未添加的群' : '选择已发现群'}</option>{availableGroups.map((group) => <option value={group} key={group}>{group}</option>)}</select><button type="button" disabled={!selectableGroup} onClick={addDiscoveredGroup}>添加</button><button type="button" className="secondary" onClick={() => void loadDiscoveredGroups()}>刷新</button></div></label><label>告警群 OpenID（每行一个）<textarea value={value.alert_groups.join('\n')} onChange={(event) => set('alert_groups', event.target.value.split(/\n|,/))} placeholder="启用后至少填写一个群 OpenID" /></label><div className="row"><label>连续离线样本<input type="number" min="1" max="20" value={value.alert_failure_samples} onChange={(event) => set('alert_failure_samples', Number(event.target.value))} /></label><label>连续恢复样本<input type="number" min="1" max="20" value={value.alert_recovery_samples} onChange={(event) => set('alert_recovery_samples', Number(event.target.value))} /></label></div><p className="muted">轮询周期固定为 3 分钟。首次启用或更换数据源只建立基线。</p>{value.alert_groups.length > 0 && <div className="alert-groups">{value.alert_groups.map((group) => <div className="alert-group" key={group}><code>{group}</code><button className="secondary" disabled={testBusy !== ''} onClick={() => void testGroup(group)}>测试发送</button>{testMessage[group] && <span className={testMessage[group] === '发送成功' ? 'test-ok' : 'test-error'}>{testMessage[group]}</span>}</div>)}</div>}</article>
			<article className="card action-card"><h2>主动测试</h2><p className="muted">模拟消息不会修改真实告警状态。</p><label>目标群<div className="action-target"><select value={actionGroup} onChange={(event) => setSelectedActionGroup(event.target.value)} disabled={actionGroups.length === 0}><option value="">{actionGroups.length === 0 ? '暂无已发现或已配置的群' : '选择目标群'}</option>{actionGroups.map((group) => <option value={group} key={group}>{group}</option>)}</select><button type="button" className="secondary" onClick={() => void loadDiscoveredGroups()}>刷新</button></div></label><div className="action-buttons"><button type="button" disabled={!actionGroup || actionBusy !== ''} onClick={() => void runAction('status')}>{actionBusy === 'status' ? '发送中...' : '发送状态图'}</button><button type="button" className="action-failure" disabled={!actionGroup || actionBusy !== ''} onClick={() => void runAction('offline')}>{actionBusy === 'offline' ? '发送中...' : '模拟故障'}</button><button type="button" className="action-recovery" disabled={!actionGroup || actionBusy !== ''} onClick={() => void runAction('recovery')}>{actionBusy === 'recovery' ? '发送中...' : '模拟恢复'}</button></div>{actionResult && <p className={actionResult.ok ? 'test-ok' : 'test-error'}>{actionResult.message}</p>}</article>
			{previewURL && <article className="card preview-card"><h2>状态图预览</h2><img src={previewURL} alt="状态图预览" /></article>}
		</div>
	</section>
}

function Logs({ logs, refresh }: { logs: Log[]; refresh: () => void }) {
  return <section className="content"><div className="title-row"><div><h1>收发日志</h1><p>仅记录事件摘要，不保存完整请求体、签名或 Access Token。</p></div><button className="secondary" onClick={refresh}>刷新</button></div><div className="log-list">{logs.length === 0 ? <div className="empty">暂无收发日志</div> : logs.map((item) => <div className="log" key={item.id}><div className="log-top"><span className={item.direction === 'send' ? 'tag send' : 'tag'}>{item.direction === 'send' ? '发送' : '接收'}</span><strong>{item.event_type}</strong><span className={`state ${item.status}`}>{item.status}</span><time>{new Date(item.created_at).toLocaleString()}</time></div><div className="log-detail">群：{item.group_openid || '-'}　消息：{item.message_id || '-'}　{item.message || ''}</div></div>)}</div></section>
}

function Bindings({ items, refresh, onDeleted }: { items: AccountBinding[]; refresh: () => void; onDeleted: (id: string) => void }) {
	const [busy, setBusy] = useState('')
	const [error, setError] = useState('')
	async function revoke(item: AccountBinding) {
		if (!window.confirm(`确认撤销 ${item.email} 的绑定？`)) return
		setBusy(item.id); setError('')
		try {
			await api(`/api/account-bindings/${encodeURIComponent(item.id)}`, { method: 'DELETE' })
			onDeleted(item.id)
		} catch (reason) {
			setError(reason instanceof Error ? reason.message : '撤销失败')
		} finally { setBusy('') }
	}
	return <section className="content"><div className="title-row"><div><h1>账号绑定</h1><p>列表只显示脱敏邮箱和非秘密元数据，撤销不会修改 GGAPI 账号。</p></div><button className="secondary" onClick={refresh}>刷新</button></div>{error && <p className="error banner">{error}</p>}<div className="binding-list">{items.length === 0 ? <div className="empty">暂无账号绑定</div> : items.map((item) => <div className="binding" key={item.id}><div><strong>{item.email}</strong><p className="muted">用户名：{item.username || '-'}　用户 ID：{item.ggapi_user_id || '-'}　首次绑定群：{item.first_group_openid || '-'}</p><time>{new Date(item.bound_at).toLocaleString()}</time></div><button className="danger" disabled={busy !== ''} onClick={() => void revoke(item)}>{busy === item.id ? '撤销中...' : '撤销绑定'}</button></div>)}</div></section>
}

createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>)
