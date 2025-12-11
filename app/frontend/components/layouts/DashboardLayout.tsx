import { Link, usePage } from '@inertiajs/react'
import { Sidebar } from '../Sidebar'
import { ModeToggle } from '../ModeToggle'
import { SharedProps } from '@/types'
import { Button } from '@/components/ui/button'
import { LogOut } from 'lucide-react'

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const { props } = usePage<SharedProps>()
  const { current_user } = props

  return (
    <div className="flex min-h-screen flex-col md:flex-row">
      <Sidebar className="hidden w-64 md:block flex-shrink-0" />

      <div className="flex-1 flex flex-col min-w-0">
        <header className="flex h-14 items-center gap-4 border-b bg-background px-6">
          <div className="ml-auto flex items-center gap-4">
            <span className="text-sm text-muted-foreground">
              {current_user?.email}
            </span>
            <ModeToggle />
            <Link href="/session" method="delete" as="button">
              <Button variant="ghost" size="icon">
                <LogOut className="h-4 w-4" />
              </Button>
            </Link>
          </div>
        </header>

        <main className="flex-1 overflow-y-auto p-6">
          {children}
        </main>
      </div>
    </div>
  )
}
