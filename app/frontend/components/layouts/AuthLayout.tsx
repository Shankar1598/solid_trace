import { ModeToggle } from '../ModeToggle'

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen grid lg:grid-cols-2">
      <div className="relative hidden h-full flex-col bg-muted p-10 text-white dark:border-r lg:flex">
        <div className="absolute inset-0 bg-primary" />
        <div className="relative z-20 flex items-center text-lg font-medium">
          <div className="mr-2 h-6 w-6 rounded-full bg-white" />
          SolidTrace
        </div>
        <div className="relative z-20 mt-auto">
          <blockquote className="space-y-2">
            <p className="text-lg">
              "Open source error tracking that helps you fix bugs faster."
            </p>
          </blockquote>
        </div>
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
