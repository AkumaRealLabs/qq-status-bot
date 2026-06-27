import type { BalanceRow } from '@/types'

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error)
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
