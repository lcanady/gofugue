import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import * as React from 'react';
import { cn } from '@renderer/lib/utils';

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-all duration-200 hover:-translate-y-[1px] active:translate-y-0 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-primary/12 disabled:pointer-events-none disabled:opacity-50 disabled:transform-none',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary-hover hover:shadow-[0_4px_12px_rgba(99,102,241,0.35)]',
        destructive: 'border border-destructive text-destructive bg-transparent hover:bg-destructive/5',
        outline:
          'border border-border bg-transparent hover:bg-secondary hover:text-foreground',
        secondary: 'border border-border bg-transparent hover:bg-secondary hover:text-foreground',
        ghost: 'hover:bg-secondary hover:text-foreground',
        link: 'text-primary underline-offset-4 hover:underline hover:transform-none',
      },
      size: {
        default: 'h-[38px] px-4 py-2',
        sm: 'h-8 px-3 text-xs',
        lg: 'h-[44px] px-8',
        icon: 'h-[38px] w-[38px]',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button';
    return (
      <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />
    );
  },
);
Button.displayName = 'Button';

export { buttonVariants };
