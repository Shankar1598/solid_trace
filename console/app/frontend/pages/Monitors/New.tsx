import { usePage } from '@inertiajs/react'
import Breadcrumbs from '@/components/Breadcrumbs'
import DashboardLayout from '@/components/layouts/DashboardLayout'
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
    <DashboardLayout breadcrumbs={<Breadcrumbs items={[{ label: 'Monitors', href: `/${orgSlug}/monitors` }, { label: 'New monitor' }]} />}>
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight">New monitor</h1>
        <MonitorForm monitor={monitor} projects={projects} integrations={integrations} orgSlug={orgSlug} />
      </div>
    </DashboardLayout>
  )
}
