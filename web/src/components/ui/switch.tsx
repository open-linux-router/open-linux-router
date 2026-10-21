import { Switch as SwitchPrimitive } from "@base-ui/react/switch"

import { cn } from "@/lib/utils"

/**
 * On or off, at one size: 44 × 24 with a 44px hit area at every breakpoint.
 *
 * `busy` is not `disabled`. A switch that is applying a change keeps its
 * contrast, says `aria-busy`, and refuses input — the adjacent status word is
 * what tells the operator something is happening. `disabled` means the router
 * will refuse the change (loopback, missing binary, no tunnel address), and it
 * is the only state that greys out.
 */
function Switch({
  className,
  busy,
  ...props
}: SwitchPrimitive.Root.Props & {
  busy?: boolean
}) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      data-busy={busy ? '' : undefined}
      aria-busy={busy || undefined}
      readOnly={busy}
      className={cn(
        "peer group/switch relative inline-flex shrink-0 items-center rounded-full border border-transparent transition-colors duration-150 outline-none group-has-[:focus-visible]/field-label:border-transparent group-has-[:focus-visible]/field-label:ring-0 after:absolute after:-inset-x-3 after:-inset-y-2 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 h-6 w-11 px-0.5 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 data-checked:bg-primary-fill data-unchecked:bg-input dark:data-unchecked:bg-input/80 data-busy:cursor-progress data-busy:data-checked:bg-primary-fill/70 data-disabled:cursor-not-allowed data-disabled:opacity-50",
        className
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className="pointer-events-none block size-[18px] rounded-full bg-white shadow-xs transition-transform duration-150 data-checked:translate-x-5 data-unchecked:translate-x-0"
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
