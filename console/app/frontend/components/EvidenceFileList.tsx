import { usePage } from '@inertiajs/react'
import { Film, ImageOff } from 'lucide-react'
import { EvidenceViewer } from '@/components/EvidenceViewer'
import { cn } from '@/lib/utils'
import { evidenceBytesPath, evidencePath, evidenceTitle, isPlainClick, isVideo, type EvidenceFile } from '@/lib/evidenceFiles'
import type { SharedProps } from '@/types'

export function EvidenceThumb({ file, className }: { file: EvidenceFile; className?: string }) {
  const slug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const mark = cn('flex items-center justify-center bg-muted text-muted-foreground', className)
  if (file.deleted_at) return <span className={mark} title="Deleted" data-mark="deleted"><ImageOff className="h-4 w-4" aria-hidden /></span>
  if (isVideo(file)) return <span className={mark} title="Video" data-mark="video"><Film className="h-4 w-4" aria-hidden /></span>
  return <img src={evidenceBytesPath(slug, file.id)} alt="" loading="lazy" className={cn('bg-muted object-cover', className)} />
}

/**
 * The Evidence files of a page, each a link to its page. A plain click opens
 * the file in the popup (`openId`) on this page; a click with Cmd, Ctrl,
 * Shift or the middle button opens its page, as a link does.
 */
export function EvidenceFileList({ files, openId, onOpenChange }: {
  files: EvidenceFile[]
  openId: string | null
  onOpenChange: (id: string | null) => void
}) {
  const slug = usePage<SharedProps>().props.current_org?.slug ?? ''
  if (files.length === 0) return null
  return (
    <section aria-label="Evidence files" data-section="evidence-files" className="space-y-2">
      <h2 className="text-sm font-semibold">Evidence files</h2>
      <ul className="flex flex-wrap gap-3">
        {files.map(file => (
          <li key={file.id} data-evidence-file={file.id}>
            <a
              href={evidencePath(slug, file.id)}
              onClick={e => {
                if (!isPlainClick(e)) return
                e.preventDefault()
                onOpenChange(file.id)
              }}
              className="flex w-40 flex-col gap-1 border p-1 text-xs hover:bg-muted/50 focus-visible:outline-2 focus-visible:outline-ring"
            >
              <EvidenceThumb file={file} className="h-24 w-full" />
              <span className="truncate" title={evidenceTitle(file)}>{evidenceTitle(file)}</span>
            </a>
          </li>
        ))}
      </ul>
      <EvidenceViewer files={files} openId={openId} onOpenChange={onOpenChange} />
    </section>
  )
}
