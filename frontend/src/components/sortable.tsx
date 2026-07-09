import { type CSSProperties, type ReactNode } from 'react'
import { closestCenter, DndContext, type DragEndEvent } from '@dnd-kit/core'
import { rectSortingStrategy, SortableContext, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { GripVertical } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useSortSensors } from '@/lib/sortable'
import { cn } from '@/lib/utils'

export function SortableGrid({
  itemIds,
  onDragEnd,
  children,
  className,
}: {
  itemIds: string[]
  onDragEnd: (event: DragEndEvent) => void
  children: ReactNode
  className?: string
}) {
  const sensors = useSortSensors()
  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
      <SortableContext items={itemIds} strategy={rectSortingStrategy}>
        <div className={cn('grid min-w-0 gap-3 md:grid-cols-2 xl:grid-cols-3', className)}>{children}</div>
      </SortableContext>
    </DndContext>
  )
}

export function SortableItem({
  id,
  children,
  className,
}: {
  id: string
  children: (bind: {
    attributes: ReturnType<typeof useSortable>['attributes']
    listeners: ReturnType<typeof useSortable>['listeners']
    isDragging: boolean
  }) => ReactNode
  className?: string
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id })
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
  }
  return (
    <div ref={setNodeRef} className={cn('min-w-0', isDragging && 'z-10 opacity-80', className)} style={style}>
      {children({ attributes, listeners, isDragging })}
    </div>
  )
}

export function DragHandle({
  sorting,
  attributes,
  listeners,
}: {
  sorting?: boolean
  attributes: ReturnType<typeof useSortable>['attributes']
  listeners: ReturnType<typeof useSortable>['listeners']
}) {
  return (
    <Button
      variant="outline"
      size="icon"
      title="拖拽排序"
      disabled={sorting}
      className="touch-none"
      {...attributes}
      {...listeners}
    >
      <GripVertical className="size-4" />
      <span className="sr-only">拖拽排序</span>
    </Button>
  )
}
