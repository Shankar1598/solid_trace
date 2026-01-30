import { Sidebar } from '../Sidebar'

export default function DashboardLayout({ children }: { children: React.ReactNode }) {

  return (
    <div className="flex min-h-screen max-h-screen flex-col md:flex-row">
      <Sidebar className="hidden md:flex flex-shrink-0" />

      <div className="flex-1 flex flex-col min-w-0">


        <main className="flex-1 overflow-y-auto p-4">
          {children}
        </main>
      </div>
    </div>
  )
}
