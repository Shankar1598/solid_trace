import { useEffect, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { format } from 'date-fns'
import { cn } from '@/lib/utils'
import type { AttributeValue, Log } from '@/lib/logs/stream'

const LINE_HEIGHT = 22
/** Start loading older Logs this many lines before the end. */
const LOAD_AHEAD = 100
const INLINE_ATTRIBUTES = 4

const LEVEL_TEXT: Record<string, string> = {
  trace: 'text-muted-foreground',
  debug: 'text-muted-foreground',
  info: 'text-sky-600 dark:text-sky-400',
  warn: 'text-amber-600 dark:text-amber-400',
  error: 'text-red-600 dark:text-red-400',
  fatal: 'text-red-700 dark:text-red-300 font-bold',
}

const LEVEL_EDGE: Record<string, string> = {
  warn: 'border-l-amber-500',
  error: 'border-l-red-500',
  fatal: 'border-l-red-700',
}

interface LogStreamProps {
  logs: Log[]
  end: 'more' | 'end' | 'capped'
  loadingOlder: boolean
  olderError: string | null
  onLoadOlder: () => void
}

/** The Logs of a run as dense terminal-style lines, newest first, virtualised. */
export function LogStream({ logs, end, loadingOlder, olderError, onLoadOlder }: LogStreamProps) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: logs.length + 1, // the last line says what comes after the Logs
    getScrollElement: () => scrollRef.current,
    estimateSize: () => LINE_HEIGHT,
    overscan: 30,
    useFlushSync: false,
  })
  const items = virtualizer.getVirtualItems()
  const lastIndex = items.length > 0 ? items[items.length - 1].index : 0

  useEffect(() => {
    if (end === 'more' && !loadingOlder && !olderError && lastIndex >= logs.length - LOAD_AHEAD) onLoadOlder()
  }, [end, loadingOlder, olderError, lastIndex, logs.length, onLoadOlder])

  return (
    <div ref={scrollRef} className="h-full overflow-auto font-mono text-[12px] leading-[22px]">
      <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {items.map(item => (
          <div
            key={item.key}
            className="absolute left-0 top-0 w-full"
            style={{ height: LINE_HEIGHT, transform: `translateY(${item.start}px)` }}
          >
            {item.index < logs.length
              ? <LogLine log={logs[item.index]} />
              : <StreamEnd end={end} loading={loadingOlder} error={olderError} count={logs.length} onRetry={onLoadOlder} />}
          </div>
        ))}
      </div>
    </div>
  )
}

function LogLine({ log }: { log: Log }) {
  const level = log.level ?? ''
  const attributes = Object.entries(log.attributes ?? {}).slice(0, INLINE_ATTRIBUTES)

  return (
    <div className={cn('flex items-center gap-2 pr-3 whitespace-nowrap border-l-[3px] hover:bg-muted/50', LEVEL_EDGE[level] ?? 'border-l-transparent')}>
      <span className="pl-2 text-muted-foreground tabular-nums" title={log.time}>{format(new Date(log.time), 'MMM dd HH:mm:ss.SSS')}</span>
      {log.service && <span className="text-fuchsia-600 dark:text-fuchsia-400">[{log.service}]</span>}
      <span className={cn('w-10 shrink-0 uppercase text-[11px]', LEVEL_TEXT[level])}>{level}</span>
      <span className="truncate">{log.message}</span>
      <span className="flex gap-2 text-muted-foreground">
        {log.environment && <Field name="environment" value={log.environment} />}
        {attributes.map(([name, value]) => <Field key={name} name={name} value={value} />)}
      </span>
    </div>
  )
}

function Field({ name, value }: { name: string; value: AttributeValue }) {
  const text = typeof value === 'string' ? value : JSON.stringify(value)
  return (
    <span>
      <span className="opacity-60">{name}=</span>
      <span className="text-foreground/80">{text.length > 60 ? `${text.slice(0, 60)}…` : text}</span>
    </span>
  )
}

function StreamEnd({ end, loading, error, count, onRetry }: {
  end: LogStreamProps['end']; loading: boolean; error: string | null; count: number; onRetry: () => void
}) {
  let content: React.ReactNode
  if (error) {
    content = <>Older Logs didn't load: {error} <button className="underline" onClick={onRetry}>Try again</button></>
  } else if (loading || end === 'more') {
    content = 'Loading older Logs…'
  } else if (end === 'capped') {
    content = `Showing the newest ${count.toLocaleString()} Logs. Narrow the time range to see older Logs.`
  } else {
    content = count === 0 ? '' : 'No older Logs in this time range.'
  }
  return <div className="px-3 text-center font-sans text-xs text-muted-foreground">{content}</div>
}
