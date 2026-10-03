interface SqlBoxProps {
  text: string
  onChange: (text: string) => void
  onRun: () => void
  /** Leaving SQL mode with edited SQL: asking before discarding it. */
  leaving: boolean
  onDiscard: () => void
  onStay: () => void
}

/** SQL mode's editable SQL, run only on Run, and the prompt to leave it. */
export function SqlBox({ text, onChange, onRun, leaving, onDiscard, onStay }: SqlBoxProps) {
  return (
    <div className="border-b bg-muted/30 px-3 py-2">
      <textarea
        value={text}
        autoFocus
        spellCheck={false}
        rows={Math.min(12, text.split('\n').length + 1)}
        onChange={e => onChange(e.target.value)}
        onKeyDown={e => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault()
            onRun()
          }
        }}
        aria-label="SQL"
        placeholder="SELECT level, count(*) FROM logs GROUP BY level"
        className="w-full resize-y rounded-md border bg-background px-2 py-1.5 font-mono text-[12px] leading-5 outline-none focus:ring-1 focus:ring-primary"
      />
      <div className="mt-0.5 text-[11px] text-muted-foreground">
        <code>logs</code> holds the Logs of the Project and time range above. SQL runs as written, only when you press Run.
      </div>
      {leaving && (
        <div className="mt-2 flex flex-wrap items-center gap-3 rounded-md border border-amber-400 bg-amber-50 p-2 text-xs dark:bg-amber-950/30">
          <span className="flex-1">
            SQL can't be turned back into a Query. Switching discards your SQL and returns to the Query it started from.
          </span>
          <button onClick={onDiscard} className="rounded border px-2 py-1 hover:bg-background">Discard and switch</button>
          <button onClick={onStay} className="rounded border px-2 py-1 hover:bg-background">Stay in SQL</button>
        </div>
      )}
    </div>
  )
}
