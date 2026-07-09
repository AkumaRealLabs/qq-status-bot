import { useCallback, useState } from 'react'
import { errorMessage } from '@/lib/format'
import { useAutoClear } from '@/lib/hooks'

export type FeedbackTone = 'neutral' | 'success' | 'error' | 'warning'

/** 根据文案与 error 标志推导 InlineMessage 语气。 */
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
 * 共享的保存/操作反馈状态：pending / success / error，成功目标文案到期自动清除。
 * 默认成功目标：`已保存`。
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

/** 变更/操作错误：用原生 alert 提示。 */
export function alertError(error: unknown) {
  window.alert(errorMessage(error))
}

/** 保存成功后稍后再关对话框，让成功文案可见。 */
export function closeAfterSave(setOpen: (open: boolean) => void, delayMs = 350) {
  window.setTimeout(() => setOpen(false), delayMs)
}
