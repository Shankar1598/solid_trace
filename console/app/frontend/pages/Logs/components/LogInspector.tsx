import { useEffect, useState } from 'react'
import { X } from 'lucide-react'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { fetchLog } from '@/lib/logs/api'
import { inspectorFields, inspectorJson, utcTime } from '@/lib/logs/inspector'
import type { Log } from '@/lib/logs/stream'
import { cn } from '@/lib/utils'
import { LEVEL_TEXT } from './LogStream'
import { Tag, valueText } from './TagMenu'

interface LogInspectorProps {
  /** The Log as the stream has it, shown until EventStore's copy arrives. */
  log: Log
  orgSlug: string
  projectSlug: string
  onClose: () => void
}

/**
 * One Log, over the right of the stream (it doesn't narrow it): Fields and
 * JSON tabs, every value a tag, and the raw time in UTC.
 */
export function LogInspector({ log: listed, orgSlug, projectSlug, onClose }: LogInspectorProps) {
  const [fetched, setFetched] = useState<{ log: Log | null; error: string | null } | null>(null)
  const log = fetched?.log ?? listed

  useEffect(() => {
    const controller = new AbortController()
    setFetched(null)
    fetchLog({ orgSlug, projectSlug, id: listed.id, time: listed.time, signal: controller.signal })
      .then(found => setFetched({ log: found, error: found ? null : 'This Log is no longer stored.' }))
      .catch((error: Error) => {
        if (!controller.signal.aborted) setFetched({ log: null, error: error.message })
      })
    return () => controller.abort()
  }, [orgSlug, projectSlug, listed.id, listed.time])

  useEffect(() => {
    const closeOnEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented) onClose()
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [onClose])

  const time = utcTime(log.time)
  const level = log.level ?? ''

  return (
    <aside
      aria-label="Log inspector"
      className="absolute inset-y-0 right-0 z-20 flex w-[460px] max-w-full flex-col border-l bg-background shadow-2xl"
    >
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <Tag field="time" value={time} className="font-mono text-xs text-muted-foreground tabular-nums">{time}</Tag>
        <div className="flex-1" />
        <button onClick={onClose} className="rounded p-1 hover:bg-muted" aria-label="Close the inspector">
          <X className="h-4 w-4" />
        </button>
      </div>

      <div className="border-b px-3 py-3">
        {level && <div className={cn('mb-1 text-[11px] font-semibold uppercase', LEVEL_TEXT[level])}>{level}</div>}
        <div className="font-mono text-sm break-words whitespace-pre-wrap">{log.message}</div>
        {fetched?.error && <div className="mt-2 text-xs text-muted-foreground">{fetched.error} Showing it as the stream loaded it.</div>}
      </div>

      <Tabs defaultValue="fields" className="min-h-0 flex-1 gap-0">
        <TabsList variant="line" className="px-3">
          <TabsTrigger value="fields">Fields</TabsTrigger>
          <TabsTrigger value="json">JSON</TabsTrigger>
        </TabsList>
        <TabsContent value="fields" className="min-h-0 overflow-auto border-t p-3">
          <table className="w-full font-mono text-xs">
            <tbody>
              {inspectorFields(log).map(({ name, value }) => (
                <tr key={name} className="border-b last:border-0">
                  <td className="py-1.5 pr-3 align-top whitespace-nowrap text-muted-foreground">{name}</td>
                  <td className="py-1.5 break-all">
                    <Tag field={name} value={value} className="whitespace-pre-wrap">{valueText(value)}</Tag>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TabsContent>
        <TabsContent value="json" className="min-h-0 overflow-auto border-t p-3">
          <pre className="font-mono text-[11px] whitespace-pre-wrap break-all">{inspectorJson(log)}</pre>
        </TabsContent>
      </Tabs>
    </aside>
  )
}
