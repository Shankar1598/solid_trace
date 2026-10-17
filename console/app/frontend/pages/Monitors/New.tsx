import { usePage } from '@inertiajs/react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import type { IntegrationOption, MonitorSettings } from '@/lib/monitors/monitor'
import type { SharedProps } from '@/types'
import type { ProjectOption } from '../Logs/components/BuilderPanel'
import { MonitorForm } from './components/MonitorForm'

/** A new Monitor, from the explorer's Project, Query and range when it came from there. */
export default function MonitorsNew({ monitor, projects, integrations }: {
  monitor: MonitorSettings
  projects: ProjectOption[]
  integrations: IntegrationOption[]
}) {
  const orgSlug = usePage<SharedProps>().props.current_org?.slug ?? ''
  return (
    <DashboardLayout>
      <PageHeader breadcrumbs={[{ label: 'Monitors', href: `/${orgSlug}/monitors` }, { label: 'New monitor' }]} title="New monitor" />
      <MonitorForm monitor={monitor} projects={projects} integrations={integrations} orgSlug={orgSlug} />
    </DashboardLayout>
  )
}
