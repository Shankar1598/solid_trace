import { useForm, usePage } from '@inertiajs/react'
import { Lock, TriangleAlert } from 'lucide-react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SharedProps } from '@/types'
import { toast } from 'sonner'

/** One setting read at start: where it comes from and the value in effect. A secret's value is only whether it's set. */
interface RestartSetting {
  label: string
  /** The env var or event_store.yml key. */
  source: string
  value: string | number | boolean | null
  default: boolean
  secret: boolean
}

/** A group of settings read at start, or why they couldn't be read. */
interface RestartGroup {
  name: string
  settings?: RestartSetting[]
  unavailable?: string
}

/** The install's settings as InstanceSettingsAnswer builds them. */
interface InstanceSettings {
  editable: {
    sql_queries_enabled: boolean
    sql_queries_description: string
    default_log_retention_days: number
    log_all_reads: boolean
  }
  sign_up: { open: boolean, command: string }
  restart_to_change: RestartGroup[]
}

/**
 * Settings → Instance, for instance admins: the settings that apply at once,
 * Sign-up (read-only), and what the install read at start.
 */
export default function SettingsInstance({ settings }: { settings: InstanceSettings }) {
  const { current_org } = usePage<SharedProps>().props
  const path = `/${current_org!.slug}/settings/instance`
  const { editable, sign_up } = settings

  const { data, setData, put, processing, errors, transform } = useForm({
    sql_queries_enabled: editable.sql_queries_enabled,
    default_log_retention_days: String(editable.default_log_retention_days),
    log_all_reads: editable.log_all_reads,
  })

  transform((data) => ({ instance_settings: data }))

  const save: React.FormEventHandler = (e) => {
    e.preventDefault()
    put(path, {
      preserveScroll: true,
      onError: () => toast.error("Instance settings weren't saved"),
    })
  }

  return (
    <SettingsLayout>
      <PageHeader
        title="Instance"
        description="How this install is set up, for every Organization."
      />
      <div className="space-y-6">
        <Card>
          <form onSubmit={save}>
            <CardHeader>
              <CardTitle>Apply at once</CardTitle>
              <CardDescription>A saved change takes effect immediately, with no restart.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6 pb-2">
              <div className="space-y-2">
                <label className="flex items-center gap-2 text-sm font-medium">
                  <Checkbox
                    aria-label="SQL queries"
                    checked={data.sql_queries_enabled}
                    onCheckedChange={(checked) => setData('sql_queries_enabled', checked === true)}
                  />
                  SQL queries
                </label>
                <pre className="whitespace-pre-wrap text-xs text-muted-foreground font-sans" data-testid="sql-queries-description">
                  {editable.sql_queries_description}
                </pre>
                {errors.sql_queries_enabled && <p className="text-sm font-medium text-destructive">{errors.sql_queries_enabled}</p>}
              </div>

              <div className="space-y-2">
                <Label htmlFor="default_log_retention_days">Default Retention (days)</Label>
                <Input
                  id="default_log_retention_days"
                  inputMode="numeric"
                  className="max-w-32"
                  value={data.default_log_retention_days}
                  onChange={(e) => setData('default_log_retention_days', e.target.value)}
                />
                {errors.default_log_retention_days && (
                  <p className="text-sm font-medium text-destructive">{errors.default_log_retention_days}</p>
                )}
                <p className="text-xs text-muted-foreground">
                  Whole days. It applies only to Projects that set no Retention of their own; EventStore's next Retention run deletes their older Logs.
                </p>
              </div>

              <div className="space-y-2">
                <label className="flex items-center gap-2 text-sm font-medium">
                  <Checkbox
                    aria-label="Log all reads"
                    checked={data.log_all_reads}
                    onCheckedChange={(checked) => setData('log_all_reads', checked === true)}
                  />
                  Log all reads
                </label>
                <p className="text-xs text-muted-foreground">
                  Times every Log read and its SQL into the Meta Organization's Project, to investigate slowness. Off, only the MCP server's reads are timed.
                </p>
                {errors.log_all_reads && <p className="text-sm font-medium text-destructive">{errors.log_all_reads}</p>}
              </div>
            </CardContent>
            <CardFooter className="justify-end">
              <Button type="submit" disabled={processing}>
                {processing ? 'Saving…' : 'Save changes'}
              </Button>
            </CardFooter>
          </form>
        </Card>

        <Card data-testid="sign-up">
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Lock className="h-4 w-4" />Sign-up</CardTitle>
            <CardDescription>
              {sign_up.open
                ? 'Open: anyone who can reach Console can make an account.'
                : 'Closed: people join by Invite. The first user can always sign up.'}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <p className="text-muted-foreground">
              Opening Sign-up lets anyone who can reach Console make an account, so it changes only from a shell on the server.
              Then a stolen instance admin session or API token can't open the install.
            </p>
            <pre className="bg-muted px-3 py-2 text-xs overflow-x-auto">{sign_up.command}</pre>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Restart to change</CardTitle>
            <CardDescription>
              Read when each service started. Change them in its environment or event_store.yml, then restart it.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            {settings.restart_to_change.map((group) => (
              <RestartGroupTable key={group.name} group={group} />
            ))}
          </CardContent>
        </Card>
      </div>
    </SettingsLayout>
  )
}

function RestartGroupTable({ group }: { group: RestartGroup }) {
  return (
    <section className="space-y-2" aria-label={group.name}>
      <h3 className="caps-label">{group.name}</h3>
      {group.unavailable ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <TriangleAlert className="h-4 w-4 text-warning" />{group.unavailable}
        </p>
      ) : (
        <dl className="divide-y text-sm">
          {group.settings!.map((setting) => (
            <div key={setting.source} className="grid gap-1 py-2 sm:grid-cols-[14rem_1fr]">
              <dt>
                <div>{setting.label}</div>
                <code className="text-xs text-muted-foreground">{setting.source}</code>
              </dt>
              <dd className="flex flex-wrap items-center gap-2 break-all">
                <span className="font-mono" data-testid={`setting-${setting.source}`}>{display(setting)}</span>
                {setting.default && <span className="text-xs text-muted-foreground">default</span>}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}

function display(setting: RestartSetting): string {
  if (setting.secret) return setting.value ? 'Set' : 'Not set'
  if (setting.value === null || setting.value === '') return 'Not set'
  return String(setting.value)
}
