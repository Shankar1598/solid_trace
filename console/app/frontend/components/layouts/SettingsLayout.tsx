import DashboardLayout from './DashboardLayout'
import { SettingsSidebar } from '../SettingsSidebar'
import { cn } from '@/lib/utils'

interface SettingsLayoutProps {
  children: React.ReactNode
  className?: string
}

export default function SettingsLayout({ children, className }: SettingsLayoutProps) {
  return (
    <DashboardLayout>
      <div className="flex h-screen -m-4 overflow-hidden">
        <SettingsSidebar />
        <main className={cn("flex-1 overflow-y-auto", className)}>
          {/* The same padding as every other page, so the title sits where theirs does. */}
          <div className="max-w-4xl p-4 pb-20">
            {children}
          </div>
        </main>
      </div>
    </DashboardLayout>
  )
}
