import { ModeToggle } from '../ModeToggle'
import FlashToasts from '../FlashToasts'

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen grid lg:grid-cols-2">
      <FlashToasts />
      {/* The brand panel is dark in both themes; the sidebar's tile is the same mark. */}
      <div className="hidden h-full flex-col bg-neutral-950 p-10 text-neutral-50 dark:border-r lg:flex">
        <div className="flex items-center gap-3 text-lg font-medium">
          <div className="flex h-8 w-8 items-center justify-center bg-neutral-50 font-mono text-sm font-semibold text-neutral-950">S</div>
          SolidTrace
        </div>
        <p className="mt-auto max-w-md text-lg text-neutral-300">
          Open source error tracking that helps you fix bugs faster.
        </p>
      </div>
      <div className="flex flex-col items-center justify-center p-8">
        <div className="absolute right-8 top-8">
          <ModeToggle />
        </div>
        <div className="w-full max-w-sm space-y-6">
          {children}
        </div>
      </div>
    </div>
  )
}
