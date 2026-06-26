import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const variants = cva(
  'inline-flex h-10 shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-sm px-5 text-sm font-medium leading-none text-foreground outline-none transition-colors focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/20 disabled:pointer-events-none disabled:bg-primary-disabled disabled:text-muted-foreground disabled:opacity-100 [&_svg]:pointer-events-none [&_svg:not([class*=size-])]:size-4',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary-active',
        outline: 'border border-border bg-background text-foreground hover:bg-card',
        ghost: 'text-muted-foreground hover:bg-card hover:text-foreground',
        secondary: 'border border-border bg-background text-foreground hover:bg-card',
        danger: 'bg-destructive text-primary-foreground hover:bg-destructive/90 focus-visible:ring-destructive/20',
      },
      size: {
        default: 'h-10 px-5',
        sm: 'h-9 gap-1.5 px-3 text-sm',
        icon: 'size-9 rounded-full px-0',
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
