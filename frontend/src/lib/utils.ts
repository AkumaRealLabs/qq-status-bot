import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** Order-sensitive id equality for drag-sort dirty checks. */
export function sameIDs(a: { id: string }[], b: { id: string }[]) {
  return a.length === b.length && a.every((item, index) => item.id === b[index]?.id)
}

export function keysOf<T>(row: { keys?: T[] } | undefined): T[] {
  return row?.keys ?? []
}
