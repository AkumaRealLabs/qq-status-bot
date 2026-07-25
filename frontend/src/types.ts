import type { ElementType } from 'react'

export type Upstream = {
  id: string
  name: string
  type: 'newapi' | 'sub2api'
  base_url: string
  enabled: boolean
  user_id?: string
  access_token?: string
  access_token_set?: boolean
  email?: string
  password?: string
  password_set?: boolean
  sub2api_access_token?: string
  sub2api_access_token_set?: boolean
  sub2api_refresh_token?: string
  sub2api_refresh_token_set?: boolean
	balance_rate: number
	low_balance_threshold: number
	runway_warning_hours?: number
  last_error?: string
}

export type ApiKey = {
  id: string
  upstream_id: string
  name: string
  key_set?: boolean
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
  error?: string
}

export type BalanceRefreshResult = {
  total: number
  succeeded: number
  failed: number
  results: { upstream_id: string; upstream_name: string; success: boolean; error?: string }[]
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

export type SettingsData = {
  check_interval_minutes: number
  telegram_bot_token?: string
  telegram_bot_token_set?: boolean
  telegram_chat_id?: string
  onebot_enabled: boolean
  onebot_base_url?: string
  onebot_http_token?: string
  onebot_http_token_set?: boolean
  onebot_webhook_token?: string
  onebot_webhook_token_set?: boolean
  onebot_group_ids: string[]
  site_name: string
  site_icon: string
  epay_base_url: string
  epay_pid: string
  epay_key: string
  epay_key_set?: boolean
  notification_rules: NotificationRules
}

export type OneBotStatus = {
  status: 'disabled' | 'unconfigured' | 'online' | 'error'
  error?: string
}

export type SiteSettings = Pick<SettingsData, 'site_name' | 'site_icon'>

export type TabID = 'balances' | 'costs' | 'revenue' | 'upstreams' | 'events' | 'audit' | 'notifications' | 'self-check' | 'settings'

export type NavTab = { id: TabID; label: string; short: string; icon: ElementType }

export type RevenueCard = {
  id: string
  name: string
  source_type: 'epay_total' | 'newapi_orders' | 'sub2api_orders'
  base_url?: string
  user_id?: string
  access_token?: string
  access_token_set?: boolean
  admin_api_key?: string
  admin_api_key_set?: boolean
  epay_pid?: string
  epay_key?: string
  epay_key_set?: boolean
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

export type RevenueOrder = {
  remote_id: string
  amount: number
  status: string
  payment_type?: string
  paid_at: string
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

export type SchedulerConfig = {
  scheduler_provider: 'ggapi' | 'axonhub'
  scheduler_base_url: string
  scheduler_user_id: string
  scheduler_access_token: string
  scheduler_access_token_set?: boolean
  /** 价格未命中托管档位时落到该 new-api 分组（不可空串） */
  scheduler_unassigned_group: string
  scheduler_tiers: SchedulerTier[]
}

export type SchedulerTier = {
  tag: string
  group: string
  price_min: number
  price_max: number
}

export type SchedulerChannel = {
  id: string
  name: string
  status: number
  priority?: number
  weight?: number
  tag?: string
  type?: string
  group?: string
  models?: string[]
  remote_status?: 'enabled' | 'disabled' | 'archived' | string
  tags?: string[]
  ordering_weight?: number
  archived?: boolean
}

export type AxonHubConfig = {
  base_url: string
  admin_email: string
  admin_password?: string
  admin_password_set?: boolean
  control_mode: 'off' | 'active'
}

export type AxonHubPreflight = {
  ok: boolean
  bound: number
  checks: { name: string; ok: boolean; message: string }[]
}

export type SchedulerGroup = {
  name: string
  ratio?: string
  description?: string
}

export type SchedulerLog = {
  id: string
  card_id: string
  card_name: string
  channel_id: string
  channel_name: string
  action: string
  status: 'success' | 'error' | 'skipped'
  message: string
  reason?: string
  provider?: 'ggapi' | 'axonhub' | string
  created_at: string
}

export type SchedulerApplyResult = {
  updated: number
  unchanged: number
  skipped: number
}

export type SchedulerCostBinding = {
  id: string
  name: string
  upstream_id?: string
  upstream_name?: string
  key_id?: string
  key_name?: string
  key_group?: string
  key_group_ratio?: string
  balance_rate?: number
  manual_cost_ratio?: string
  source_type: 'upstream_key' | 'manual'
  effective_cost?: number
  cost_available: boolean
  missing_reason?: string
  ggapi_external_takeover: boolean
  ggapi_ownership_reason?: string
  axonhub_external_takeover: boolean
  axonhub_ownership_reason?: string
  scheduler_channel_id?: string
  scheduler_channel_name?: string
  axonhub_channel_id?: string
  axonhub_channel_name?: string
  enabled: boolean
  created_at?: string
  updated_at?: string
}

export type CostBindingForm = Pick<
  SchedulerCostBinding,
  | 'name'
  | 'upstream_id'
  | 'key_id'
  | 'manual_cost_ratio'
  | 'source_type'
  | 'scheduler_channel_id'
  | 'scheduler_channel_name'
  | 'axonhub_channel_id'
  | 'axonhub_channel_name'
  | 'enabled'
>

export type NotificationRules = {
  enabled: boolean
  event_types: Record<string, boolean>
  failure_threshold: number
  recovery: boolean
}

export type OpsEvent = {
  id: string
  type: string
  severity: 'info' | 'warning' | 'success' | 'error' | string
  title: string
  message: string
  target_type?: string
  target_id?: string
  actions: string[]
  read: boolean
  acked: boolean
  created_at: string
  updated_at: string
}

export type OpsEventGroup = {
  type: string
  target_type?: string
  target_id?: string
  count: number
  unread_count: number
  unacked_count: number
  latest: OpsEvent
}

export type AuditLog = {
  id: string
  actor: string
  action: string
  target_type?: string
  target_id?: string
  summary: string
  fields?: string[]
  created_at: string
}

export type SelfCheckResponse = {
  checked_at: string
  items: { name: string; status: 'ok' | 'warn' | 'error' | 'safe_mode' | string; message?: string }[]
}
