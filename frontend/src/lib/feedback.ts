import { useCallback, useState } from 'react'
import { errorMessage } from '@/lib/format'
import { useAutoClear } from '@/lib/hooks'

export type FeedbackTone = 'neutral' | 'success' | 'error' | 'warning'

/** Derive InlineMessage tone from text + error flag. */
export function feedbackTone(
  message: string,
  options?: {
    error?: boolean
    success?: string | string[]
  },
): FeedbackTone {
  if (!message) return 'neutral'
  if (options?.error) return 'error'
  const success = options?.success
  const list = Array.isArray(success) ? success : success ? success.split('|') : ['已保存']
  if (list.includes(message)) return 'success'
  if (
    message.endsWith('完成') ||
    message.endsWith('已发送') ||
    message.endsWith('已导出') ||
    message.endsWith('已刷新') ||
    message.startsWith('更新 ')
  ) {
    return 'success'
  }
  return 'neutral'
}

/**
 * Shared save/action feedback state: pending / success / error + auto-clear on success targets.
 * Default success targets: `已保存`.
 */
export function useFeedback(successTargets = '已保存') {
  const [message, setMessage] = useState('')
  useAutoClear(message, successTargets, setMessage)
  const clear = useCallback(() => setMessage(''), [])
  const pending = useCallback((text = '保存中...') => setMessage(text), [])
  const success = useCallback((text = '已保存') => setMessage(text), [])
  const fail = useCallback((error: unknown) => setMessage(errorMessage(error)), [])
  const tone = useCallback((isError = false) => feedbackTone(message, { error: isError, success: successTargets }), [message, successTargets])
  return { message, setMessage, clear, pending, success, fail, tone }
}

export function confirmDelete(name: string, note?: string) {
  return window.confirm(`确认删除 ${name}？${note ? `\n${note}` : ''}`)
}

export function secretPlaceholder(saved?: boolean, empty = '') {
  return saved ? '已保存，不修改请留空' : empty
}

/** Mutation / action error via native alert. */
export function alertError(error: unknown) {
  window.alert(errorMessage(error))
}

/** Close dialog shortly after a successful save so the success label is visible. */
export function closeAfterSave(setOpen: (open: boolean) => void, delayMs = 350) {
  window.setTimeout(() => setOpen(false), delayMs)
}
