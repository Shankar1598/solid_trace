import { Link, usePage } from '@inertiajs/react'
import { Copy } from 'lucide-react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { FormSection } from '@/components/FormSection'
import { Button } from '@/components/ui/button'
import { copyText } from '@/components/copyText'
import { SharedProps } from '@/types'

// Organization settings → Agent sessions → Capture setup (CaptureSetupsController).
interface CaptureSetupProps {
  on: boolean
  captured_repositories: string[]
  version: string
  version_note: string | null
  install_command: string
  plugin_commands: string[]
  plugin_check_command: string
  managed_settings: string
  login_command: string
  status_command: string
  telemetry: {
    user_settings: string
    managed_settings: string
    has_key: boolean
    project_slug: string
  } | null
}

const DOCS = 'https://github.com/Shankar1598/solid_trace/blob/main/docs/agent-sessions.md'

// A block of text to copy: a command or a settings file.
function Snippet({ code, what }: { code: string; what: string }) {
  return (
    <div className="space-y-2">
      <div className="flex justify-end">
        <Button variant="outline" size="sm" onClick={() => copyText(code, what)} aria-label={`Copy the ${what.toLowerCase()}`}>
          <Copy className="h-3.5 w-3.5" /> Copy
        </Button>
      </div>
      <pre className="overflow-x-auto border bg-muted/40 p-3 font-mono text-xs leading-5">{code}</pre>
    </div>
  )
}

export default function SettingsCaptureSetup(props: CaptureSetupProps) {
  const { current_org } = usePage<SharedProps>().props
  const settingsPath = `/${current_org!.slug}/settings/agent_sessions`

  return (
    <SettingsLayout>
      <PageHeader
        title="Capture setup"
        description="Turn on capture on a laptop or a dev VM. Claude Code then uploads each Agent session in a captured repository to this Organization."
      />

      <FormSection title="Captured repositories" description="Only Agent sessions in these repositories upload. Each member of the Organization can read them.">
        {!props.on ? (
          <p className="text-sm text-muted-foreground">
            Agent sessions are off, so no Agent session uploads. An Admin turns them on in{' '}
            <Link href={settingsPath} className="underline underline-offset-4">Agent sessions</Link>.
          </p>
        ) : props.captured_repositories.length === 0 ? (
          <p className="text-sm text-muted-foreground">None, so no Agent session uploads.</p>
        ) : (
          <ul className="space-y-1 font-mono text-sm">
            {props.captured_repositories.map(entry => <li key={entry}>{entry}</li>)}
          </ul>
        )}
      </FormSection>

      <FormSection
        className="mt-6"
        title="1. Install the CLI"
        description={`The solidtrace CLI of this instance's version, ${props.version}. The script checks the SHA-256 of the file before it installs it in ~/.local/bin.`}
      >
        {props.version_note && <p className="text-xs text-muted-foreground">{props.version_note}</p>}
        <Snippet code={props.install_command} what="Install command" />
      </FormSection>

      <FormSection
        className="mt-6"
        title="2. Install the plugin"
        description="The Claude Code plugin starts the upload when a turn or an Agent session ends. It holds no URL and no secret."
      >
        <p className="text-sm text-muted-foreground">
          Claude Code adds the SolidTrace marketplace from GitHub at this instance's release, and clones only the folders of the marketplace and the plugin.
        </p>
        <Snippet code={props.plugin_commands.join('\n')} what="Plugin commands" />
        <p className="text-sm text-muted-foreground">
          Then check that the list shows solidtrace@solidtrace as enabled:
        </p>
        <Snippet code={props.plugin_check_command} what="Plugin check command" />
        <p className="text-sm text-muted-foreground">
          Or, if your team uses Claude Code's managed settings, add this to them. Each machine then gets the plugin, and a person cannot turn it off. Claude Code gets the plugin when it starts on a machine, and the plugin runs from the next start.
        </p>
        <Snippet code={props.managed_settings} what="Managed settings" />
      </FormSection>

      <FormSection
        className="mt-6"
        title="3. Sign in"
        description="Opens Console in the browser (or gives a code for the device page on a machine with no browser). The approval page tells you which repositories upload."
      >
        <Snippet code={props.login_command} what="Login command" />
      </FormSection>

      <FormSection
        className="mt-6"
        title="4. Check"
        description="Lists the Agent sessions on the machine as uploaded, not in the list, or failed."
      >
        <Snippet code={props.status_command} what="Status command" />
        <p className="text-sm text-muted-foreground">
          More in the <a href={DOCS} className="underline underline-offset-4" target="_blank" rel="noreferrer">Agent sessions docs</a>.
        </p>
      </FormSection>

      <FormSection
        id="telemetry"
        className="mt-6"
        title="5. Send telemetry (optional)"
        description="Claude Code then sends its OpenTelemetry events and metrics to the Agent Project, as Logs. Ingest keeps only the records of a captured repository."
      >
        {!props.telemetry ? (
          <p className="text-sm text-muted-foreground">Agent sessions are off, so there is no Agent Project to send telemetry to.</p>
        ) : (
          <>
            {!props.telemetry.has_key && (
              <p className="text-sm text-muted-foreground">
                The Agent Project has no key yet, so the blocks show &lt;key&gt;. An Admin makes the key on the{' '}
                <Link href={`/${current_org!.slug}/projects/${props.telemetry.project_slug}`} className="underline underline-offset-4">Agent Project's page</Link>.
              </p>
            )}
            <p className="text-sm text-muted-foreground">
              Add this to <code className="font-mono text-xs">~/.claude/settings.json</code> on the machine. Claude Code reads it when it starts.
            </p>
            <Snippet code={props.telemetry.user_settings} what="User settings" />
            <p className="text-sm text-muted-foreground">
              Or, if your team uses Claude Code's managed settings, use this in place of the block of step 2. It pushes the plugin and the telemetry to each machine.
            </p>
            <Snippet code={props.telemetry.managed_settings} what="Managed settings with telemetry" />
            <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
              <li>
                The block does not set <code className="font-mono text-xs">OTEL_LOG_USER_PROMPTS</code> or <code className="font-mono text-xs">OTEL_LOG_TOOL_DETAILS</code>,
                because they send prompt text and Bash commands. So the records hold counts, durations, tool names and the estimated cost.
              </li>
              <li>
                Claude Code reads only the <code className="font-mono text-xs">origin</code> remote. So the records of a captured repository arrive
                only from a clone whose <code className="font-mono text-xs">origin</code> is that repository.
              </li>
              <li>When the captured repositories list is empty, Ingest drops every record.</li>
              <li>A change to the captured repositories applies to the next request.</li>
            </ul>
          </>
        )}
      </FormSection>
    </SettingsLayout>
  )
}
