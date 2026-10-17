import { usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
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
    <DashboardLayout>
      <PageHeader
        breadcrumbs={[
          { label: 'Monitors', href: `/${orgSlug}/monitors` },
          { label: monitor.name, href: `/${orgSlug}/monitors/${monitor.id}` },
          { label: 'Edit' },
        ]}
        title={`Edit ${monitor.name}`}
      />
      <MonitorForm monitor={monitor} monitorId={monitor.id} projects={projects} integrations={integrations} orgSlug={orgSlug} />
    </DashboardLayout>
  )
}
