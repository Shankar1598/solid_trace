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
        <main className={cn("flex-1 overflow-y-auto bg-background/50", className)}>
          <div className="max-w-5xl mx-auto py-10 px-8 mb-20">
            {children}
          </div>
        </main>
      </div>
    </DashboardLayout>
  )
}
