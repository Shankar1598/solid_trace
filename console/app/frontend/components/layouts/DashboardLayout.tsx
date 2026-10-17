import { Sidebar } from '../Sidebar'
import FlashToasts from '../FlashToasts'
import { CommandPalette } from '../CommandPalette'
import { useShortcutListener } from '../useShortcut'

interface DashboardLayoutProps {
  children: React.ReactNode
}

/** The signed-in frame: the sidebar and the page. A page opens with a PageHeader, which holds its breadcrumbs. */
export default function DashboardLayout({ children }: DashboardLayoutProps) {
  useShortcutListener()
  return (
    <CommandPalette>
      <div className="flex min-h-screen max-h-screen flex-col md:flex-row">
        <FlashToasts />
        <Sidebar className="hidden md:flex flex-shrink-0" />

        <div className="flex-1 flex flex-col min-w-0">
          <main className="flex-1 overflow-y-auto p-4">
            {children}
          </main>
        </div>
      </div>
    </CommandPalette>
  )
}
