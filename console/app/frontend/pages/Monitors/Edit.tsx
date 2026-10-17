import { usePage } from '@inertiajs/react'
import Breadcrumbs from '@/components/Breadcrumbs'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import type { IntegrationOption, Monitor } from '@/lib/monitors/monitor'
import type { SharedProps } from '@/types'
import type { ProjectOption } from '../Logs/components/BuilderPanel'
import { MonitorForm } from './components/MonitorForm'

/** Edits a Monitor. */
export default function MonitorsEdit({ monitor, projects, integrations }: {
  monitor: Monitor
  projects: ProjectOption[]
  integrations: IntegrationOption[]
}) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  return (
    <DashboardLayout
      breadcrumbs={<Breadcrumbs items={[
        { label: 'Monitors', href: `/${orgSlug}/monitors` },
        { label: monitor.name, href: `/${orgSlug}/monitors/${monitor.id}` },
        { label: 'Edit' },
      ]} />}
    >
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight">Edit {monitor.name}</h1>
        <MonitorForm monitor={monitor} monitorId={monitor.id} projects={projects} integrations={integrations} orgSlug={orgSlug} />
      </div>
    </DashboardLayout>
  )
}
