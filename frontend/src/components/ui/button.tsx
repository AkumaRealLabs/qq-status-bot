import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const variants = cva(
  'inline-flex h-9 items-center justify-center gap-2 rounded-md px-3 text-sm font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-zinc-900 text-white hover:bg-zinc-800 focus-visible:outline-zinc-900',
        outline: 'border border-zinc-300 bg-white text-zinc-900 hover:bg-zinc-100',
        ghost: 'text-zinc-700 hover:bg-zinc-100',
        danger: 'bg-red-600 text-white hover:bg-red-700 focus-visible:outline-red-600',
      },
      size: {
        default: 'h-9 px-3',
        sm: 'h-8 px-2 text-xs',
        icon: 'size-9 px-0',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<'button'> & VariantProps<typeof variants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot : 'button'
  return <Comp className={cn(variants({ variant, size }), className)} {...props} />
}
