import { Link, usePage } from '@inertiajs/react'
import { Film, ImageOff } from 'lucide-react'
import { cn } from '@/lib/utils'
import { evidenceBytesPath, evidencePath, evidenceTitle, isVideo, type EvidenceFile } from '@/lib/evidenceFiles'
import type { SharedProps } from '@/types'

/** A small image of a screenshot, or a mark for a video or a deleted file. */
export function EvidenceThumb({ file, className }: { file: EvidenceFile; className?: string }) {
  const slug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const mark = cn('flex items-center justify-center bg-muted text-muted-foreground', className)
  if (file.deleted_at) return <span className={mark} title="Deleted"><ImageOff className="h-4 w-4" aria-hidden /></span>
  if (isVideo(file)) return <span className={mark} title="Video"><Film className="h-4 w-4" aria-hidden /></span>
  return <img src={evidenceBytesPath(slug, file.id)} alt="" loading="lazy" className={cn('bg-muted object-cover', className)} />
}

/** The Evidence files of an Agent session, each a link to its page; nothing when it has none. */
export function EvidenceFileList({ files }: { files: EvidenceFile[] }) {
  const slug = usePage<SharedProps>().props.current_org?.slug ?? ''
  if (files.length === 0) return null
  return (
    <section aria-label="Evidence files" data-section="evidence-files" className="space-y-2">
      <h2 className="text-sm font-semibold">Evidence files</h2>
      <ul className="flex flex-wrap gap-3">
        {files.map(file => (
          <li key={file.id} data-evidence-file={file.id}>
            <Link href={evidencePath(slug, file.id)} className="flex w-40 flex-col gap-1 border p-1 text-xs hover:bg-muted/50">
              <EvidenceThumb file={file} className="h-24 w-full" />
              <span className="truncate" title={evidenceTitle(file)}>{evidenceTitle(file)}</span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}
