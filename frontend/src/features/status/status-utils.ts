import { fmtTime } from '@/lib/format'
import type { ModelCard, Probe, PublicModelCard } from '@/types'

export function probeStatus(probe: Probe) {
  return probe.status || (probe.success ? 'operational' : 'failed')
}

export function probeOK(probe: Probe) {
  const status = probeStatus(probe)
  return ['operational', 'degraded', '正常', '延迟偏高'].includes(status)
}

export function emptyHistorySlots(windowValue: string) {
  return ({ '1h': 12, '3h': 18, '5h': 20, '1d': 24, '7d': 28, '15d': 30 } as Record<string, number>)[windowValue] || 24
}

function historyWindowMilliseconds(windowValue: string) {
  return ({ '1h': 60, '3h': 180, '5h': 300, '1d': 1440, '7d': 10080, '15d': 21600 } as Record<string, number>)[windowValue] * 60 * 1000 || 60 * 60 * 1000
}

function probeTime(probe: Probe) {
  const value = Date.parse(probe.checked_at)
  return Number.isFinite(value) ? value : 0
}

// 每个时间桶只展示一个代表探测，避免探测频率差异把某张卡的格子撑得更多。
export function bucketProbeHistory(history: Probe[], windowValue: string, now = Date.now()): Array<Probe | undefined> {
  const slotCount = emptyHistorySlots(windowValue)
  const slots: Array<Probe | undefined> = Array.from({ length: slotCount })
  const start = now - historyWindowMilliseconds(windowValue)
  const slotDuration = historyWindowMilliseconds(windowValue) / slotCount
  for (const probe of history) {
    const checkedAt = probeTime(probe)
    if (checkedAt < start || checkedAt > now) continue
    const index = Math.min(slotCount - 1, Math.floor((checkedAt - start) / slotDuration))
    const existing = slots[index]
    if (!existing || (probeOK(existing) && !probeOK(probe)) || (probeOK(existing) === probeOK(probe) && probeTime(probe) > probeTime(existing))) {
      slots[index] = probe
    }
  }
  return slots
}

export function probeStatusLabel(status: string) {
  return ({
    operational: '正常',
    degraded: '延迟偏高',
    failed: '请求失败',
    error: '探测异常',
    正常: '正常',
    延迟偏高: '延迟偏高',
    验证失败: '验证失败',
    请求失败: '请求失败',
    探测异常: '探测异常',
  } as Record<string, string>)[status] || status || '-'
}

export function probeHoverTitle(probe: Probe) {
  return [
    `状态：${probeStatusLabel(probeStatus(probe))}`,
    `延迟：${probe.latency_ms} ms`,
    `HTTP 状态：${probe.http_status || '-'}`,
    `测什么：${probePurpose(probe)}`,
    `探活输入：${probe.input || '-'}`,
    `模型回答：${probe.output || '-'}`,
    `检查时间：${fmtTime(probe.checked_at)}`,
  ].filter(Boolean).join('\n')
}

export function isModelCard(card: ModelCard | PublicModelCard): card is ModelCard {
  return 'id' in card
}

export function cardTitle(card: ModelCard | PublicModelCard, ratio: string) {
  if (!isModelCard(card)) return card.name
  if (card.base_url) return card.name
  const detail = ratio !== '-' ? ratio : card.key_name || card.key_group || ''
  return detail ? `${card.upstream_name || card.name} · ${detail}` : card.name
}

export function groupCards<T extends ModelCard | PublicModelCard>(cards: T[]) {
  const groups: { name: string; cards: T[] }[] = []
  for (const card of cards) {
    const name = cardDisplayGroup(card)
    let group = groups.find((item) => item.name === name)
    if (!group) {
      group = { name, cards: [] }
      groups.push(group)
    }
    group.cards.push(card)
  }
  return groups
}

export function cardDisplayGroup(card: ModelCard | PublicModelCard) {
  return card.display_group?.trim() || '其他'
}

export function probePurpose(_probe: Probe) {
  return '检查 gpt-5.6-sol 响应与连通性'
}
