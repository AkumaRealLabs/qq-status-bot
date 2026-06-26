import { requestClient } from '#/api/request';

export interface ListResponse<T> {
  items: T[];
  page: number;
  perPage: number;
  totalItems: number;
  totalPages: number;
}

export interface UpstreamRecord {
  id: string;
  name: string;
  type: 'newapi' | 'sub2api';
  base_url: string;
  enabled: boolean;
  user_id: string;
  access_token: string;
  email: string;
  password: string;
  sub2api_access_token: string;
  sub2api_refresh_token: string;
  balance_rate: number;
  low_balance_threshold: number;
  selected_key: string;
  last_error: string;
  failure_count: number;
  created?: string;
  updated?: string;
}

export interface SettingsRecord {
  id: string;
  check_interval_minutes?: number;
  telegram_bot_token: string;
  telegram_chat_id: string;
}

export interface SummaryKey {
  description: string;
  group: string;
  group_ratio: string;
  id: string;
  name: string;
  status: string;
}

export interface ModelCardPayload {
  enabled?: boolean;
  key: string;
  model: string;
  name?: string;
  upstream: string;
}

export interface SummaryRow {
  id: string;
  name: string;
  type: 'newapi' | 'sub2api';
  enabled: boolean;
  selected_key: string;
  last_error: string;
  failure_count: number;
  balance?: number;
  used?: number;
  remain?: number;
  last_check?: string;
  ping_success?: boolean;
  latency_ms?: number;
  http_status?: number;
  probe_error?: string;
  keys?: SummaryKey[];
}

export interface StatusRow {
  id: string;
  name: string;
  type: 'newapi' | 'sub2api';
  upstream: string;
  upstream_name: string;
  key: string;
  key_description: string;
  key_group: string;
  key_group_ratio: string;
  key_name: string;
  enabled: boolean;
  requests: number;
  success: number;
  failed: number;
  success_rate: number;
  avg_latency: number;
  last_check?: string;
  last_error: string;
  last_http_status?: number;
  last_latency?: number;
  last_probe_error?: string;
  last_success?: boolean;
  keys?: SummaryKey[];
  history?: {
    checked_at: string;
    latency_ms: number;
    success: boolean;
  }[];
  model?: string;
}

export interface StatusSummary {
  avg_latency: number;
  failed: number;
  requests: number;
  rows: StatusRow[];
  success: number;
  success_rate: number;
  window: string;
}

export interface BalanceRow {
  id: string;
  name: string;
  type: 'newapi' | 'sub2api';
  enabled: boolean;
  balance?: number;
  used?: number;
  remain?: number;
  source_balance?: number;
  source_used?: number;
  source_remain?: number;
  requests?: number;
  balance_rate: number;
  low_balance_threshold: number;
  low_balance?: boolean;
  last_check?: string;
  error?: string;
}

export function listUpstreams() {
  return requestClient.get<ListResponse<UpstreamRecord>>(
    '/collections/upstreams/records',
    {
      params: { page: 1, perPage: 200, sort: 'name' },
    },
  );
}

export function createUpstream(data: Partial<UpstreamRecord>) {
  return requestClient.post<UpstreamRecord>(
    '/collections/upstreams/records',
    data,
  );
}

export function updateUpstream(id: string, data: Partial<UpstreamRecord>) {
  return requestClient.request<UpstreamRecord>(
    `/collections/upstreams/records/${id}`,
    { data, method: 'PATCH' },
  );
}

export function listSettings() {
  return requestClient.get<ListResponse<SettingsRecord>>(
    '/collections/settings/records',
    {
      params: { page: 1, perPage: 1 },
    },
  );
}

export function createSettings(data: Partial<SettingsRecord>) {
  return requestClient.post<SettingsRecord>(
    '/collections/settings/records',
    data,
  );
}

export function updateSettings(id: string, data: Partial<SettingsRecord>) {
  return requestClient.request<SettingsRecord>(
    `/collections/settings/records/${id}`,
    { data, method: 'PATCH' },
  );
}

export function getSummary() {
  return requestClient.get<SummaryRow[]>('/monitor/summary');
}

export function getStatus(window: string) {
  return requestClient.get<StatusSummary>('/monitor/status', {
    params: { window },
  });
}

export function getBalances() {
  return requestClient.get<BalanceRow[]>('/monitor/balances');
}

export function getUpstreamKeys(id: string) {
  return requestClient.get<SummaryKey[]>(`/monitor/upstreams/${id}/keys`);
}

export function createModelCard(data: ModelCardPayload) {
  return requestClient.post<StatusRow>('/monitor/cards', data);
}

export function updateModelCard(id: string, data: ModelCardPayload) {
  return requestClient.request<StatusRow>(`/monitor/cards/${id}`, {
    data,
    method: 'PATCH',
  });
}

export function deleteModelCard(id: string) {
  return requestClient.delete(`/monitor/cards/${id}`);
}

export function checkModelCard(id: string) {
  return requestClient.post(`/monitor/cards/${id}/check`);
}

export function checkUpstream(id: string) {
  return requestClient.post(`/monitor/upstreams/${id}/check`);
}

export function syncKeys(id: string) {
  return requestClient.post(`/monitor/upstreams/${id}/sync-keys`);
}

export function browserLogin(id: string) {
  return requestClient.post<{ url: string; vnc_url?: string }>(
    `/monitor/upstreams/${id}/browser-login`,
  );
}

export function browserCapture(id: string) {
  return requestClient.post<{ access_token: boolean; refresh_token: boolean }>(
    `/monitor/upstreams/${id}/browser-capture`,
  );
}

export function selectKey(id: string, keyID: string) {
  return requestClient.post(`/monitor/upstreams/${id}/selected-key`, {
    key_id: keyID,
  });
}
