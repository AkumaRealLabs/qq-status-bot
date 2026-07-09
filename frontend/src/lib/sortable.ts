import {
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import { arrayMove, sortableKeyboardCoordinates } from '@dnd-kit/sortable'

export type { DragEndEvent }

export function useSortSensors() {
  return useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
}

/** 按 dnd-kit 拖拽结束重排列表；无移动时返回 null。 */
export function reorderByDragEnd<T extends { id: string }>(items: T[], event: DragEndEvent): T[] | null {
  const { active, over } = event
  if (!over || active.id === over.id) return null
  const oldIndex = items.findIndex((item) => item.id === active.id)
  const newIndex = items.findIndex((item) => item.id === over.id)
  if (oldIndex < 0 || newIndex < 0) return null
  return arrayMove(items, oldIndex, newIndex)
}
