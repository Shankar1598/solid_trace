import { useState } from 'react'
import { useForm, usePage } from '@inertiajs/react'
import { Lock, TriangleAlert } from 'lucide-react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SharedProps } from '@/types'
import { fallbackHint, requiredScheme, type UrlFallback } from '@/lib/publicUrls'
import type { PasskeyOffer } from '@/lib/passkeys'
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
    default_trace_retention_days: number
    log_all_reads: boolean
    log_mcp_reads: boolean
    send_console_issues: boolean
    trace_console: boolean
    logs_text_match_budget_mb: number
    logs_read_threads: number
    local_cache_hours: number
    /** null when blank; the fallback is what a blank one uses. */
    console_url: string | null
    console_url_fallback: UrlFallback | null
    ingest_url: string | null
    ingest_url_fallback: UrlFallback | null
  }
  sign_up: { open: boolean, command: string }
  passkeys: PasskeyOffer
  restart_to_change: RestartGroup[]
}

/**
 * Settings → Instance, for instance admins: the settings that apply at once,
 * Sign-up and Passkeys (read-only), and what the install read at start.
 */
export default function SettingsInstance({ settings }: { settings: InstanceSettings }) {
  const { current_org } = usePage<SharedProps>().props
  const path = `/${current_org!.slug}/settings/instance`
  const { editable, sign_up } = settings

  const { data, setData, put, processing, errors, transform } = useForm({
    sql_queries_enabled: editable.sql_queries_enabled,
    default_log_retention_days: String(editable.default_log_retention_days),
    default_trace_retention_days: String(editable.default_trace_retention_days),
    log_all_reads: editable.log_all_reads,
    log_mcp_reads: editable.log_mcp_reads,
    send_console_issues: editable.send_console_issues,
    trace_console: editable.trace_console,
    logs_text_match_budget_mb: String(editable.logs_text_match_budget_mb),
    logs_read_threads: String(editable.logs_read_threads),
    local_cache_hours: String(editable.local_cache_hours),
    console_url: editable.console_url ?? '',
    ingest_url: editable.ingest_url ?? '',
  })
  const consoleRows = settings.restart_to_change.find((group) => group.name === 'Console')?.settings
  const sampleRate = consoleRows?.find((setting) => setting.source === 'SENTRY_TRACES_SAMPLE_RATE')?.value
  const scheme = requiredScheme(consoleRows?.find((setting) => setting.label === 'HTTPS')?.value)
  const bucket = hasBucket(settings.restart_to_change)
  const [confirmingHours, setConfirmingHours] = useState(false)
  const oldHours = editable.local_cache_hours
  const newHours = Number(data.local_cache_hours)
  // What a Console URL on another host would strand, as the save that was
  // refused said it: the Passkeys made for the current host, with the counts.
  const [strandedPasskeys, setStrandedPasskeys] = useState<string | null>(null)

  const send = (confirmPasskeysStopWorking = false) => {
    setConfirmingHours(false)
    setStrandedPasskeys(null)
    transform((data) => ({
      instance_settings: data,
      ...(confirmPasskeysStopWorking && { confirm_passkeys_stop_working: true }),
    }))
    put(path, {
      preserveScroll: true,
      onError: (errors) => {
        if (errors.passkeys_stop_working) setStrandedPasskeys(errors.passkeys_stop_working)
        else toast.error("Instance settings weren't saved")
      },
    })
  }

  // A change of the hours on local disk, to a value that would save, is
  // confirmed first; any other value is sent for its error.
  const save: React.FormEventHandler = (e) => {
    e.preventDefault()
    const hoursChange = /^\d+$/.test(data.local_cache_hours.trim()) && newHours !== oldHours && newHours <= 720
    if (hoursChange) setConfirmingHours(true)
    else send()
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
                <Label htmlFor="console_url">Console URL</Label>
                <Input
                  id="console_url"
                  inputMode="url"
                  placeholder={editable.console_url_fallback?.url ?? 'https://console.example.com'}
                  value={data.console_url}
                  onChange={(e) => setData('console_url', e.target.value)}
                />
                {errors.console_url && <p className="text-sm font-medium text-destructive">{errors.console_url}</p>}
                <p className="text-xs text-muted-foreground" data-testid="console-url-description">
                  Where people reach Console, as http(s)://host[:port] with no path. Emails, invite links, the MCP URL
                  and the MCP server&apos;s links use it from the next one.
                  {scheme && ` It starts with ${scheme}://, as Console runs ${scheme === 'https' ? 'with' : 'without'} HTTPS, which changes only with CONSOLE_URL and a restart (below).`}
                </p>
                <p className="text-xs text-muted-foreground" data-testid="console-url-fallback">
                  {fallbackHint(editable.console_url_fallback)}
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="ingest_url">Ingest URL</Label>
                <Input
                  id="ingest_url"
                  inputMode="url"
                  placeholder={editable.ingest_url_fallback?.url ?? 'https://ingest.example.com:4443'}
                  value={data.ingest_url}
                  onChange={(e) => setData('ingest_url', e.target.value)}
                />
                {errors.ingest_url && <p className="text-sm font-medium text-destructive">{errors.ingest_url}</p>}
                <p className="text-xs text-muted-foreground" data-testid="ingest-url-description">
                  Where SDKs and shippers send, as http(s)://host[:port][/path]. DSNs, the Logs snippets and the MCP
                  server&apos;s get_project_setup use it at once; DSNs already in apps don&apos;t change.
                </p>
                <p className="text-xs text-muted-foreground" data-testid="ingest-url-fallback">
                  {fallbackHint(editable.ingest_url_fallback)}
                </p>
              </div>

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
                <Label htmlFor="default_log_retention_days">Default Log Retention (days)</Label>
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
                <Label htmlFor="default_trace_retention_days">Default trace Retention (days)</Label>
                <Input
                  id="default_trace_retention_days"
                  inputMode="numeric"
                  className="max-w-32"
                  value={data.default_trace_retention_days}
                  onChange={(e) => setData('default_trace_retention_days', e.target.value)}
                />
                {errors.default_trace_retention_days && (
                  <p className="text-sm font-medium text-destructive">{errors.default_trace_retention_days}</p>
                )}
                <p className="text-xs text-muted-foreground">
                  Whole days that traces' Spans are kept, for Projects that set none of their own. EventStore's next Retention run deletes older Spans.
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="logs_text_match_budget_mb">Text-match budget (MiB)</Label>
                <Input
                  id="logs_text_match_budget_mb"
                  inputMode="numeric"
                  className="max-w-32"
                  value={data.logs_text_match_budget_mb}
                  onChange={(e) => setData('logs_text_match_budget_mb', e.target.value)}
                />
                {errors.logs_text_match_budget_mb && (
                  <p className="text-sm font-medium text-destructive">{errors.logs_text_match_budget_mb}</p>
                )}
                <p className="text-xs text-muted-foreground" data-testid="text-match-budget-description">
                  The most a Query that matches text (search, contains, startswith or =~ on message, or a function of message)
                  may read, in MiB of Logs over its range and the files its filters on service and file keep. The Logs explorer
                  and the MCP server refuse a Query over it, with narrower ways to run it. Dashboard Charts and Monitors are never refused. A Query over a quarter of it is a heavy
                  read, limited by Heavy Log reads at once below. 0 refuses no Query.
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="logs_read_threads">Log read threads</Label>
                <Input
                  id="logs_read_threads"
                  inputMode="numeric"
                  className="max-w-32"
                  value={data.logs_read_threads}
                  onChange={(e) => setData('logs_read_threads', e.target.value)}
                />
                {errors.logs_read_threads && (
                  <p className="text-sm font-medium text-destructive">{errors.logs_read_threads}</p>
                )}
                <p className="text-xs text-muted-foreground" data-testid="read-threads-description">
                  The threads ClickHouse may use for each Log read: Queries, histograms, Fields, Monitors, traces and
                  Transactions, not SQL queries. More threads read the bucket faster but use more CPU, which ingest shares.
                  0 to 64; 0 is ClickHouse's default of one per CPU core. It applies from the next read.
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="local_cache_hours">Hours on local disk</Label>
                <Input
                  id="local_cache_hours"
                  inputMode="numeric"
                  className="max-w-32"
                  disabled={bucket === false}
                  value={data.local_cache_hours}
                  onChange={(e) => setData('local_cache_hours', e.target.value)}
                />
                {errors.local_cache_hours && (
                  <p className="text-sm font-medium text-destructive">{errors.local_cache_hours}</p>
                )}
                <p className="text-xs text-muted-foreground" data-testid="local-cache-hours-description">
                  How long Logs and Spans stay on local disk before they move to the bucket, 0 to 720; 0 moves each part
                  once stored. Older Logs are read from the bucket, which is slower. It applies within a minute.
                </p>
                {bucket === false && (
                  <p className="text-xs text-muted-foreground" data-testid="local-cache-hours-no-bucket">
                    EventStore has no bucket, so every Log stays on local disk and this does nothing.
                  </p>
                )}
                {bucket === null && (
                  <p className="text-xs text-muted-foreground" data-testid="local-cache-hours-no-bucket">
                    EventStore's config couldn't be read, so whether it has a bucket isn't known. Without one this does nothing.
                  </p>
                )}
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
                  Times every Log read and its SQL into the Meta Organization's Project, to investigate slowness.
                </p>
                {errors.log_all_reads && <p className="text-sm font-medium text-destructive">{errors.log_all_reads}</p>}
              </div>

              <div className="space-y-2">
                {/* While Log all reads is on it times the MCP server's reads too, so this shows on and waits. */}
                <label className="flex items-center gap-2 text-sm font-medium has-[:disabled]:text-muted-foreground">
                  <Checkbox
                    aria-label="Log MCP reads"
                    checked={data.log_all_reads || data.log_mcp_reads}
                    disabled={data.log_all_reads}
                    onCheckedChange={(checked) => setData('log_mcp_reads', checked === true)}
                  />
                  Log MCP reads
                </label>
                <p className="text-xs text-muted-foreground">
                  {data.log_all_reads
                    ? 'Log all reads times the MCP server\'s reads too; this applies once it\'s off.'
                    : 'Times the MCP server\'s Log reads and their SQL. Its tool calls are always timed.'}
                </p>
                {errors.log_mcp_reads && <p className="text-sm font-medium text-destructive">{errors.log_mcp_reads}</p>}
              </div>

              <div className="space-y-2">
                <label className="flex items-center gap-2 text-sm font-medium">
                  <Checkbox
                    aria-label="Send Console's Issues"
                    checked={data.send_console_issues}
                    onCheckedChange={(checked) => setData('send_console_issues', checked === true)}
                  />
                  Send Console&apos;s Issues
                </label>
                <p className="text-xs text-muted-foreground">
                  Sends Console&apos;s own errors to its Sentry DSN, the Meta Organization&apos;s Project, where they become Issues.
                </p>
                {errors.send_console_issues && <p className="text-sm font-medium text-destructive">{errors.send_console_issues}</p>}
              </div>

              <div className="space-y-2">
                <label className="flex items-center gap-2 text-sm font-medium">
                  <Checkbox
                    aria-label="Trace Console"
                    checked={data.trace_console}
                    onCheckedChange={(checked) => setData('trace_console', checked === true)}
                  />
                  Trace Console
                </label>
                <p className="text-xs text-muted-foreground">
                  {sampleRate
                    ? `Traces ${Number(sampleRate).toLocaleString('en', { style: 'percent', maximumFractionDigits: 2 })} of Console's requests and jobs into the Meta Organization's Project. The share is SENTRY_TRACES_SAMPLE_RATE, below.`
                    : 'SENTRY_TRACES_SAMPLE_RATE is not set, so Console traces nothing whatever this says.'}
                </p>
                {errors.trace_console && <p className="text-sm font-medium text-destructive">{errors.trace_console}</p>}
              </div>
            </CardContent>
            <CardFooter className="justify-end">
              <Button type="submit" disabled={processing}>
                {processing ? 'Saving…' : 'Save changes'}
              </Button>
            </CardFooter>
          </form>
        </Card>

        <AlertDialog open={confirmingHours} onOpenChange={setConfirmingHours}>
          <AlertDialogContent data-testid="local-cache-hours-confirm">
            <AlertDialogHeader>
              <AlertDialogTitle>Change Hours on local disk from {oldHours} to {newHours}?</AlertDialogTitle>
              <AlertDialogDescription>
                Logs older than {newHours} hours are read from the bucket, which is slower than local disk;
                lowering it moves more of the recent Logs there within a minute or two.
              </AlertDialogDescription>
              <p className="text-sm text-muted-foreground">
                Raising it keeps more on local disk, which counts toward the disk limit; parts already in the bucket stay there.
              </p>
              <p className="text-sm text-muted-foreground">
                ClickHouse recomputes every part's move time in the background, reading each part's time column from the
                bucket, so change it rarely.
              </p>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <Button onClick={() => send()}>Change to {newHours} hours</Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        <AlertDialog open={strandedPasskeys !== null} onOpenChange={(open) => !open && setStrandedPasskeys(null)}>
          <AlertDialogContent data-testid="passkeys-stop-working-confirm">
            <AlertDialogHeader>
              <AlertDialogTitle>Change the Console URL&apos;s host?</AlertDialogTitle>
              <AlertDialogDescription>{strandedPasskeys}</AlertDialogDescription>
              <p className="text-sm text-muted-foreground">
                Their users then sign in with another factor or a Recovery code, and can add Passkeys for the new host
                on Settings → User.
              </p>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <Button variant="destructive" onClick={() => send(true)}>Change the host</Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

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

        <Card data-testid="passkeys">
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Lock className="h-4 w-4" />Passkeys</CardTitle>
            <CardDescription>
              {settings.passkeys.offered
                ? `Offered at ${settings.passkeys.host}: users add them on Settings → User and sign in with them at ${settings.passkeys.url}.`
                : `Not offered. ${settings.passkeys.reason}`}
            </CardDescription>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Passkeys are made for the Console URL's host: they need it on https:// with a domain (or http://localhost),
            and stop working if the host changes.
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

/**
 * Whether EventStore has a bucket, from its config's rows: true or false, or
 * null when its config couldn't be read.
 */
function hasBucket(groups: RestartGroup[]): boolean | null {
  const eventStore = groups.find((group) => group.name === 'EventStore')?.settings
  if (!eventStore) return null
  return eventStore.some((setting) => setting.source === 'object_store.bucket')
}
