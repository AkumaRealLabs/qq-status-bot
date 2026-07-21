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
	balance_guard_mode?: 'observe' | 'active'
	balance_close_threshold?: number
	balance_recover_threshold?: number
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

export type ModelCard = {
  id: string
  name: string
  base_url?: string
  api_key?: string
  api_key_set?: boolean
  model: string
  upstream_id?: string
  upstream_name: string
  key_id?: string
  key_name?: string
  key_group?: string
  key_group_ratio?: string
  effective_ratio?: string
  display_group: string
  pool_enabled: boolean
  manual_cost_ratio?: string
  scheduler_group?: string
  scheduler_channel_id?: string
  scheduler_channel_name?: string
  scheduler_auto_disabled: boolean
  scheduler_auto_disabled_at?: string
  probe_muted: boolean
  enabled: boolean
  public_enabled: boolean
  sort_order: number
  last_error: string
  history?: Probe[]
}

export type PublicModelCard = Pick<ModelCard, 'name' | 'display_group' | 'probe_muted' | 'last_error' | 'history'> & {
  auto_probe_paused: boolean
}

export type Probe = {
  checked_at: string
  status: string
  input?: string
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
  telegram_bot_token_set?: boolean
  telegram_chat_id?: string
  onebot_enabled: boolean
  onebot_base_url?: string
  onebot_http_token?: string
  onebot_http_token_set?: boolean
  onebot_webhook_token?: string
  onebot_webhook_token_set?: boolean
  onebot_group_ids: string[]
  probe_model: string
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

export type TabID = 'status' | 'balances' | 'revenue' | 'profit' | 'messages' | 'upstreams' | 'scheduler' | 'pools' | 'events' | 'audit' | 'notifications' | 'self-check' | 'settings'

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

export type TGSessionStatus = {
  configured: boolean
  authorized: boolean
  phone?: string
  api_id?: number
  password_needed: boolean
  last_error?: string
}

export type TGChannel = {
  id: string
  display_name: string
  identifier: string
  username?: string
  peer_id: number
  access_hash?: number
  avatar_url?: string
  enabled: boolean
  message_limit: number
  pinned_only: boolean
  last_sync_at?: string
  last_error?: string
}

export type TGMessage = {
  id: string
  channel_id: string
  channel_name?: string
  remote_id: number
  published_at: string
  text: string
  media_type?: string
  media_url?: string
  media_cached: boolean
  link?: string
}

export type TGLoginForm = {
  api_id: string
  api_hash: string
  phone: string
  code: string
  password: string
}

export type CardForm = {
  name: string
  source: 'custom' | 'upstream'
  base_url: string
  api_key: string
  model: string
  upstream_id: string
  key_id: string
  display_group: string
  pool_enabled: boolean
  manual_cost_ratio: string
  enabled: boolean
  public_enabled: boolean
}

export type SchedulerConfig = {
  scheduler_base_url: string
  scheduler_user_id: string
  scheduler_access_token: string
  scheduler_access_token_set?: boolean
  /** 价格未命中托管档位时落到该 new-api 分组（不可空串） */
  scheduler_unassigned_group: string
  scheduler_tiers: SchedulerTier[]
  scheduler_traffic_mode: 'off' | 'observe' | 'active'
  scheduler_traffic_profile: 'balanced'
  scheduler_log_poll_seconds: number
}

export type SchedulerTier = {
  tag: string
  group: string
  price_min: number
  price_max: number
  sale_price: number
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
  action: 'disable' | 'restore' | 'group_sync' | 'traffic_control'
  status: 'success' | 'error' | 'skipped'
  message: string
	reason?: string
  created_at: string
}

export type TrafficWindow = {
  channel_id: string
  channel_name?: string
  model: string
  group?: string
  window_start: string
  window_end: string
  requests: number
  successes: number
  soft_failures: number
  hard_failures: number
  auth_failures: number
  user_errors: number
  p95_ttft_ms: number
  avg_ttft_ms: number
  failure_rate: number
}

export type TrafficChannelState = {
  channel_id: string
  channel_name?: string
  managed: boolean
  state: string
  reason?: string
  desired_status: number
  actual_status: number
  base_priority: number
  actual_priority: number
  base_weight: number
  actual_weight: number
  health_score: number
  healthy_baseline_ttft_ms?: number
  model?: string
  window_15s?: TrafficWindow
  window_1m?: TrafficWindow
  window_5m?: TrafficWindow
  updated_at?: string
}

export type TrafficStatus = {
  mode: 'off' | 'observe' | 'active'
  profile: 'balanced'
  connected: boolean
  last_poll_at?: string
  last_event_at?: string
  lag_seconds: number
  backlog_pages: number
  frozen: boolean
  freeze_reason?: string
  channels: TrafficChannelState[]
}

export type AvailabilityPolicy = {
  balance_guard_mode: 'observe' | 'active'
  low_balance_threshold: number
  balance_close_threshold: number
  balance_recover_threshold: number
  runway_warning_hours: number
}

export type AvailabilityBlocker = {
  kind: 'balance_low' | 'quota_exhausted' | 'probe_failed' | string
  since: string
  message?: string
  observed?: boolean
}

export type BalanceRunway = {
  hours_remaining?: number
  rate_per_hour?: number
  samples: number
  warning: boolean
}

export type AvailabilityRow = {
  channel_id: string
  channel_name: string
  card_id: string
  card_name: string
  upstream_id: string
  upstream_name: string
  managed: boolean
  blockers: AvailabilityBlocker[]
  desired_status: number
  actual_status: number
  disabled_at?: string
  recovery_success_count: number
  override?: 'force_enable' | 'manual_hold' | string
  override_until?: string
  pending_action?: string
  pending_status?: number
  retry_at?: string
  retry_count: number
  last_error?: string
  state: 'healthy' | 'warning' | 'suspect' | 'blocked' | 'recovering' | 'forced_on' | 'manual_off' | 'action_failed' | 'unmanaged' | string
  balance_fresh: boolean
  balance_remain?: number
  runway: BalanceRunway
}

export type SchedulerApplyResult = {
  updated: number
  unchanged: number
  skipped: number
}

export type CLIProxyConfig = {
  name: string
  base_url: string
  management_key?: string
  management_key_set: boolean
  enabled: boolean
}

export type CLIProxyAccount = {
  name: string
  provider?: string
  type?: string
  status?: string
  status_message?: string
  email?: string
  account_type?: string
  account?: string
  source?: string
  auth_index?: string
  size?: number
  modtime?: string
  created_at?: string
  updated_at?: string
  last_refresh?: string
  success: number
  failed: number
  recent_requests?: unknown
  runtime_only?: boolean
  disabled?: boolean
  unavailable?: boolean
}

export type CLIProxyQuotaWindow = {
  id: string
  label: string
  used_percent?: number
  remaining_percent?: number
  reset_at?: string
}

export type CLIProxyQuota = {
  plan_type?: string
  subscription_active_until?: string
  rate_limit_reset_credits_available?: number
  windows: CLIProxyQuotaWindow[]
}

export type NotificationRules = {
  enabled: boolean
  event_types: Record<string, boolean>
  failure_threshold: number
  mute_failure_threshold: number
  internal_retry_count: number
  internal_retry_interval_ms: number
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

export type ProfitResponse = {
  window: string
  revenue: number
  cost: number
  profit: number
  missing_revenue: number
  complete: boolean
  pools: ProfitPoolRow[]
  upstream_cost: { upstream_id: string; name: string; cost: number }[]
  note: string
}

export type ProfitPoolRow = {
  group: string
  tag: string
  sale_price: number
  usage: number
  revenue: number
  cost: number
  profit: number
  missing_revenue: number
  complete: boolean
  channels: ProfitChannelRow[]
}

export type ProfitChannelRow = {
  channel_id: string
  channel_name: string
  card_id?: string
  card_name?: string
  upstream_id?: string
  upstream_name?: string
  key_id?: string
  key_name?: string
  cost_per_unit?: number
  cost_source?: string
  cost_effective_from?: string
  sale_effective_from?: string
  usage: number
  revenue: number
  cost: number
  profit: number
  complete: boolean
  missing_reason?: string
}

export type SelfCheckResponse = {
  checked_at: string
  items: { name: string; status: 'ok' | 'warn' | 'error' | 'safe_mode' | string; message?: string }[]
}
