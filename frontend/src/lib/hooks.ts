import { useEffect } from 'react'

export function useAutoClear(value: string, targets: string, clear: (value: string) => void) {
  useEffect(() => {
    if (!targets.split('|').includes(value)) return
    const timer = window.setTimeout(() => clear(''), 1800)
    return () => window.clearTimeout(timer)
  }, [clear, targets, value])
}
