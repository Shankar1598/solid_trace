import { Search, X } from 'lucide-react'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { Builder, Chip } from '@/lib/logs/builder'
import { cn } from '@/lib/utils'
import { valueText } from './TagMenu'

/**
 * What the Builder shows: its chips once validate has read them. A Query the
 * Builder can't show opens in Query mode instead.
 */
export type BuilderView =
  | { status: 'reading' }
  | { status: 'ready'; builder: Builder }

export interface ProjectOption {
  id: number
  name: string
  slug: string
}

interface BuilderPanelProps {
  view: BuilderView
  project: ProjectOption
  projects: ProjectOption[]
  onProject: (slug: string) => void
  onFlip: (index: number) => void
  onRemove: (index: number) => void
  onSearch: (text: string) => void
  onRun: () => void
}

/**
 * The Builder, a row per part of its Query in the Query's order: the Project
 * whose Logs it reads, its filter chips, then the search box.
 */
export function BuilderPanel({ view, project, projects, onProject, onFlip, onRemove, onSearch, onRun }: BuilderPanelProps) {
  return (
    <div className="grid grid-cols-[5.5rem_1fr] items-center gap-x-3 gap-y-1.5 border-b px-3 py-2 font-mono text-xs">
      <Label>Project</Label>
      <div>
        <ProjectSelect project={project} projects={projects} onChange={onProject} />
      </div>
      <Label>Where</Label>
      {view.status === 'reading' ? (
        <span className="text-muted-foreground">Reading the filters…</span>
      ) : (
        <div className="flex min-h-8 flex-wrap items-center gap-1.5">
          {view.builder.filters.map((chip, i) => (
            <ChipView key={`${i}:${chip.field}`} chip={chip} onFlip={() => onFlip(i)} onRemove={() => onRemove(i)} />
          ))}
          {view.builder.filters.length === 0 && (
            <span className="text-muted-foreground">Click a value in the Logs to filter on it.</span>
          )}
        </div>
      )}
      {view.status === 'ready' && (
        <>
          <Label>Search</Label>
          <form
            className="flex h-8 max-w-xl items-center gap-2 rounded-md border px-2"
            onSubmit={e => { e.preventDefault(); onRun() }}
          >
            <Search className="h-3.5 w-3.5 text-muted-foreground" />
            <input
              value={view.builder.search}
              onChange={e => onSearch(e.target.value)}
              placeholder="Search messages and Attributes…"
              aria-label="Search Logs"
              className="flex-1 bg-transparent text-xs outline-none"
            />
          </form>
        </>
      )}
    </div>
  )
}

function Label({ children }: { children: React.ReactNode }) {
  return <span className="text-muted-foreground">{children}</span>
}

/** The Project by its slug, as the Query names it. */
function ProjectSelect({ project, projects, onChange }: { project: ProjectOption; projects: ProjectOption[]; onChange: (slug: string) => void }) {
  const items = Object.fromEntries(projects.map(p => [p.slug, p.slug]))
  return (
    <Select value={project.slug} onValueChange={v => v && onChange(v as string)} items={items}>
      <SelectTrigger className="h-8 w-fit font-mono text-xs" aria-label="Project"><SelectValue /></SelectTrigger>
      <SelectContent>
        {projects.map(p => (
          <SelectItem key={p.slug} value={p.slug}>
            <span className="font-mono">{p.slug}</span>
            {p.name !== p.slug && <span className="text-muted-foreground">{p.name}</span>}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function ChipView({ chip, onFlip, onRemove }: { chip: Chip; onFlip: () => void; onRemove: () => void }) {
  const hides = chip.op === '!='
  return (
    <span className="inline-flex max-w-80 items-center overflow-hidden rounded-full border bg-muted/50 font-mono text-xs">
      <span className="truncate py-1 pl-2.5 pr-1">{chip.field}</span>
      <button
        className={cn('px-1 py-1 font-bold', hides ? 'text-red-600' : 'text-emerald-600')}
        onClick={onFlip}
        title={hides ? 'Show only this instead' : 'Hide this instead'}
      >
        {hides ? '≠' : '='}
      </button>
      <span className="truncate py-1 pr-1" title={`a ${typeof chip.value}`}>
        {valueText(chip.value)}
      </span>
      <button className="px-1.5 py-1 hover:bg-muted" onClick={onRemove} aria-label={`Remove ${chip.field}`}>
        <X className="h-3 w-3" />
      </button>
    </span>
  )
}
