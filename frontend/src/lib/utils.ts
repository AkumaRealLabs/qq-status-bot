import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** 顺序敏感的 id 相等判断，用于拖拽排序脏检查。 */
export function sameIDs(a: { id: string }[], b: { id: string }[]) {
  return a.length === b.length && a.every((item, index) => item.id === b[index]?.id)
}

export function keysOf<T>(row: { keys?: T[] } | undefined): T[] {
  return row?.keys ?? []
}
