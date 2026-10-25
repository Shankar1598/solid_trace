import DashboardLayout from './DashboardLayout'
import { SettingsSidebar, SettingsTabs } from '../SettingsSidebar'
import { cn } from '@/lib/utils'

interface SettingsLayoutProps {
  children: React.ReactNode
  className?: string
}

export default function SettingsLayout({ children, className }: SettingsLayoutProps) {
  return (
    <DashboardLayout>
      {/* A phone scrolls the whole page under a row of tabs; wider, the sidebar stays put and only the page scrolls. */}
      <div className="flex flex-col md:flex-row -m-4 md:h-screen md:overflow-hidden">
        <SettingsSidebar />
        <SettingsTabs />
        <main className={cn("flex-1 md:overflow-y-auto", className)}>
          {/* The same padding as every other page, so the title sits where theirs does. */}
          <div className="max-w-4xl p-4 pb-20">
            {children}
          </div>
        </main>
      </div>
    </DashboardLayout>
  )
}
