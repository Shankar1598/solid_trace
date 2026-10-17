import { mergeProps } from "@base-ui/react/merge-props"
import { useRender } from "@base-ui/react/use-render"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

// One badge for every state and label: its tone says what it means, in
// sentence case. A status (an Issue's, a Monitor's) also gets a round dot.
const badgeVariants = cva(
  "h-5 gap-1.5 rounded-none border border-transparent px-1.5 text-xs font-medium has-data-[icon=inline-end]:pr-1 has-data-[icon=inline-start]:pl-1 [&>svg]:size-3! inline-flex items-center justify-center w-fit whitespace-nowrap shrink-0 [&>svg]:pointer-events-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px] transition-colors overflow-hidden group/badge",
  {
    variants: {
      variant: {
        neutral: "border-border text-muted-foreground [a]:hover:bg-muted",
        info: "bg-info/10 text-info",
        warn: "bg-warning/10 text-warning",
        error: "bg-destructive/10 text-destructive dark:bg-destructive/20",
        ok: "bg-success/10 text-success",
        count: "bg-muted text-foreground tabular-nums",
      },
    },
    defaultVariants: {
      variant: "neutral",
    },
  }
)

function Badge({
  className,
  variant = "neutral",
  dot = false,
  render,
  children,
  ...props
}: useRender.ComponentProps<"span"> & VariantProps<typeof badgeVariants> & { dot?: boolean }) {
  return useRender({
    defaultTagName: "span",
    props: mergeProps<"span">(
      {
        className: cn(badgeVariants({ className, variant })),
        children: dot ? <><span aria-hidden className="size-1.5 shrink-0 rounded-full bg-current" />{children}</> : children,
      },
      props
    ),
    render,
    state: {
      slot: "badge",
      variant,
    },
  })
}

export { Badge, badgeVariants }
