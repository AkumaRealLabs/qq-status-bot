import type { ElementType } from 'react'

export type Upstream = {
  id: string
  name: string
  type: 'newapi' | 'sub2api'
  base_url: string
  enabled: boolean
  user_id?: string
  access_token?: string
  email?: string
  password?: string
  sub2api_access_token?: string
  sub2api_refresh_token?: string
  balance_rate: number
  low_balance_threshold: number
  last_error?: string
}

export type ApiKey = {
  id: string
  upstream_id: string
  name: string
  status: string
  description: string
  group: string
  group_ratio: string
}

export type UpstreamRow = {
  upstream: Upstream
  keys: ApiKey[]
  balance?: {
    remain: number
    checked_at: string
    error: string
  }
}

export type ModelCard = {
  id: string
  name: string
  base_url?: string
  api_key?: string
  upstream_id?: string
  upstream_name: string
  key_id?: string
  key_name?: string
  key_group?: string
  key_group_ratio?: string
  effective_ratio?: string
  enabled: boolean
  public_enabled: boolean
  sort_order: number
  last_error: string
  history?: Probe[]
}

export type PublicModelCard = Pick<ModelCard, 'name' | 'last_error' | 'history'>

export type Probe = {
  checked_at: string
  status: string
  input?: string
  expected_answer?: string
  output?: string
  success: boolean
  latency_ms: number
  http_status: number
  error: string
}

export type BalanceRow = {
  id: string
  name: string
  type: string
  enabled: boolean
  balance_rate: number
  remain?: number
  source_remain?: number
  low_balance?: boolean
  last_check?: string
}

export type RechargeMethod = {
  type: string
  name: string
  min_amount?: number
  max_amount?: number
  external_url?: string
  direct: boolean
  sdk_only?: boolean
}

export type RechargeCapabilities = {
  online_enabled: boolean
  redeem_enabled: boolean
  external_url?: string
  methods: RechargeMethod[]
}

export type RechargeResult = {
  result_type: 'link' | 'qr' | 'sdk' | 'redeem' | 'order' | string
  payment_type: string
  remote_order_id?: string
  status?: string
  url?: string
  qr_code?: string
  message?: string
}

export type RechargeLog = {
  id: string
  method: string
  amount: number
  payment_type: string
  remote_order_id: string
  status: string
  message: string
  raw_status?: string
  created_at: string
}

export type MonitorStatus = {
  window: string
  requests: number
  success: number
  failed: number
  success_rate: number
  avg_latency: number
  rows: ModelCard[]
}

export type PublicMonitorStatus = Omit<MonitorStatus, 'rows'> & {
  rows: PublicModelCard[]
}

export type SettingsData = {
  check_interval_minutes: number
  telegram_bot_token?: string
  telegram_chat_id?: string
  probe_model: string
  site_name: string
  site_icon: string
  epay_base_url: string
  epay_pid: string
  epay_key: string
}

export type SiteSettings = Pick<SettingsData, 'site_name' | 'site_icon'>

export type TabID = 'status' | 'balances' | 'revenue' | 'upstreams' | 'settings'

export type NavTab = { id: TabID; label: string; short: string; icon: ElementType }

export type RevenueCard = {
  id: string
  name: string
  source_type: 'epay_total' | 'newapi_orders' | 'sub2api_orders'
  base_url?: string
  user_id?: string
  access_token?: string
  admin_api_key?: string
  epay_pid?: string
  epay_key?: string
  upstream_id?: string
  upstream_name?: string
  enabled: boolean
  sort_order: number
}

export type RevenueRow = RevenueCard & {
  revenue: number
  checked_at?: string
  error?: string
}

export type RevenueCardForm = {
  name: string
  source_type: RevenueCard['source_type']
  base_url: string
  user_id: string
  access_token: string
  admin_api_key: string
  epay_pid: string
  epay_key: string
  enabled: boolean
}

export type CardForm = {
  name: string
  source: 'custom' | 'upstream'
  base_url: string
  api_key: string
  upstream_id: string
  key_id: string
  enabled: boolean
  public_enabled: boolean
}
