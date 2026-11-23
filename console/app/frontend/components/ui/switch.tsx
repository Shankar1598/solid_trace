import { Switch as SwitchPrimitive } from "@base-ui/react/switch"

import { cn } from "@/lib/utils"

/**
 * An on/off switch for a setting that takes effect at once. It has
 * role="switch" and aria-checked, Space and Enter turn it, and a label
 * whose `for` is its `id` turns it on a click.
 */
function Switch({ className, ...props }: SwitchPrimitive.Root.Props) {
  return (
    // A native button, as in Checkbox, so that an `id` and a label's `for` reach the visible switch,
    // not the visually hidden input.
    <SwitchPrimitive.Root
      nativeButton
      render={<button type="button" />}
      data-slot="switch"
      className={cn(
        "border-input bg-input/40 dark:bg-input/30 data-checked:bg-primary dark:data-checked:bg-primary data-checked:border-primary focus-visible:border-ring focus-visible:ring-ring/50 peer relative inline-flex h-4 w-7 shrink-0 items-center rounded-none border transition-colors outline-none focus-visible:ring-1 after:absolute after:-inset-x-3 after:-inset-y-2 disabled:cursor-not-allowed disabled:opacity-50",
        className
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className="bg-muted-foreground data-checked:bg-primary-foreground pointer-events-none block size-3 translate-x-px transition-transform data-checked:translate-x-[13px]"
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
