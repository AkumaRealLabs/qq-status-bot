import type { BalanceRow } from '@/types'

export function errorMessage(error: unknown) {
  const msg = error instanceof Error ? error.message : String(error)
  if (/order not found/i.test(msg)) return '订单不存在或上游暂未查到订单'
  return msg
}

export function num(value: number | undefined) {
  if (value === undefined || Number.isNaN(value)) return '-'
  return Number(value).toFixed(2)
}

export function latestRefreshTime(rows: BalanceRow[]) {
  const latest = rows.reduce((max, row) => {
    const value = row.last_check ? new Date(row.last_check).getTime() : Number.NaN
    return Number.isNaN(value) || value <= max ? max : value
  }, 0)
  return latest ? new Date(latest).toISOString() : ''
}

export function fmtTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', { hour12: false })
}

/** Safe display time: empty / Go zero-time → `-`. */
export function displayTime(value?: string) {
  if (!value || value.startsWith('0001-')) return '-'
  return fmtTime(value)
}

/** Compact zh-CN time for dense UI (quota reset, etc.). */
export function fmtShortTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}
