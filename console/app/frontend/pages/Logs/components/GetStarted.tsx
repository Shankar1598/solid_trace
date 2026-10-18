import { Link } from '@inertiajs/react'
import { Copy, Send } from 'lucide-react'
import { useState } from 'react'
import { copyText } from '@/components/copyText'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { fetchLogPage, sendTestLog } from '@/lib/logs/api'
import { snippets, waitFor, type IngestTarget } from '@/lib/logs/getStarted'

type Sending =
  | { status: 'idle' }
  | { status: 'sending' }
  | { status: 'waiting' }
  | { status: 'late' }
  | { status: 'error'; message: string }

/**
 * An empty Project's first steps, in place of the stream: snippets that send
 * its Logs, with its ingest URL and key filled in, and Send a test Log, which
 * sends one and, once it can be read, calls onSent so the page shows it.
 */
export function GetStarted({ target, orgSlug, projectSlug, projectQuery, onSent }: {
  target: IngestTarget
  orgSlug: string
  projectSlug: string
  /** The Project's Logs unfiltered, which a test Log is looked for in. */
  projectQuery: string
  onSent: () => void
}) {
  const [sending, setSending] = useState<Sending>({ status: 'idle' })
  const shown = snippets(target)

  const send = async () => {
    setSending({ status: 'sending' })
    try {
      const time = await sendTestLog({ orgSlug, projectSlug })
      setSending({ status: 'waiting' })
      const minute = 60_000
      const readable = await waitFor(async () => {
        const page = await fetchLogPage({
          orgSlug, projectSlug, query: projectQuery, from: new Date(time.getTime() - minute), to: new Date(time.getTime() + minute),
        })
        return page.logs.length > 0
      })
      setSending(readable ? { status: 'idle' } : { status: 'late' })
      if (readable) onSent()
    } catch (error) {
      setSending({ status: 'error', message: (error as Error).message })
    }
  }

  const busy = sending.status === 'sending' || sending.status === 'waiting'
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4 p-6 text-sm">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-medium">Send this Project its first Logs</h2>
        <p className="text-muted-foreground">
          No Logs in this Project's Retention yet. Send some with one of these, or send a test Log to see one here now.
        </p>
      </div>

      {target.key === null && (
        <p role="alert" className="text-destructive">
          This Project has no key yet, so nothing can send it Logs.{' '}
          <Link href={`/${orgSlug}/projects/${projectSlug}`} className="underline">Generate one on its page</Link>, then come back.
        </p>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <Button size="sm" onClick={send} disabled={busy || target.key === null}>
          <Send className="h-3.5 w-3.5" />
          {sending.status === 'sending' ? 'Sending…' : sending.status === 'waiting' ? 'Waiting for it…' : 'Send a test Log'}
        </Button>
        {sending.status === 'late' && (
          <span className="text-muted-foreground">The test Log was sent but isn't readable yet. Run again in a moment.</span>
        )}
        {sending.status === 'error' && <span role="alert" className="text-destructive">{sending.message}</span>}
      </div>

      <Tabs defaultValue={shown[0].id} className="gap-0">
        <TabsList variant="line">
          {shown.map(s => <TabsTrigger key={s.id} value={s.id}>{s.label}</TabsTrigger>)}
        </TabsList>
        {shown.map(s => (
          <TabsContent key={s.id} value={s.id} className="flex flex-col gap-2 border-t pt-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs text-muted-foreground">{s.note}</span>
              <Button variant="outline" size="sm" onClick={() => copyText(s.code, 'Snippet')} aria-label={`Copy the ${s.label} snippet`}>
                <Copy className="h-3.5 w-3.5" /> Copy
              </Button>
            </div>
            <pre className="overflow-x-auto border bg-muted/40 p-3 font-mono text-xs leading-5">{s.code}</pre>
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}
