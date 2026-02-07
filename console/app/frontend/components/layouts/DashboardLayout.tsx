import { Sidebar } from '../Sidebar'
import FlashToasts from '../FlashToasts'

interface DashboardLayoutProps {
  children: React.ReactNode
  breadcrumbs?: React.ReactNode
  reserveBreadcrumbSpace?: boolean
}

export default function DashboardLayout({ children, breadcrumbs, reserveBreadcrumbSpace = false }: DashboardLayoutProps) {
  return (
    <div className="flex min-h-screen max-h-screen flex-col md:flex-row">
      <FlashToasts />
      <Sidebar className="hidden md:flex flex-shrink-0" />

      <div className="flex-1 flex flex-col min-w-0">


        <main className="flex-1 overflow-y-auto p-4">
          {(breadcrumbs || reserveBreadcrumbSpace) && (
            <div className="mb-4 min-h-[24px]">
              {breadcrumbs}
            </div>
          )}
          {children}
        </main>
      </div>
    </div>
  )
}
