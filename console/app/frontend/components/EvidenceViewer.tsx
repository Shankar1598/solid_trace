import { Link, usePage } from '@inertiajs/react'
import { ChevronLeft, ChevronRight, ImageOff } from 'lucide-react'
import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Segmented } from '@/components/ui/segmented'
import { evidenceBytesPath, evidenceDeletedText, evidencePath, evidenceTitle, isVideo, readSpeed, saveSpeed, SPEEDS, type EvidenceFile, type Speed } from '@/lib/evidenceFiles'
import { browserStorage } from '@/lib/recentQueries'
import { cn } from '@/lib/utils'
import type { SharedProps } from '@/types'

const SPEED_OPTIONS = SPEEDS.map(speed => ({ value: speed, label: `${speed}x` }))

/** A video of an Evidence file, with the speed control. The speed is kept for the next video. */
export function EvidencePlayer({ src, title, className }: { src: string; title: string; className?: string }) {
  const video = useRef<HTMLVideoElement>(null)
  const [speed, setSpeed] = useState(() => readSpeed(browserStorage()))
  useEffect(() => {
    if (!video.current) return
    video.current.defaultPlaybackRate = Number(speed)
    video.current.playbackRate = Number(speed)
  }, [speed])
  const change = (next: Speed) => {
    saveSpeed(browserStorage(), next)
    setSpeed(next)
  }
  return (
    <div className="space-y-2">
      <video ref={video} src={src} controls preload="metadata" aria-label={title} className={cn('max-w-full border bg-black', className)} data-evidence="video" />
      <Segmented label="Speed" value={speed} options={SPEED_OPTIONS} onChange={change} />
    </div>
  )
}

/** The notice of an Evidence file that Evidence Retention deleted. */
export function EvidenceDeletedNotice({ createdAt, deletedAt }: { createdAt: string; deletedAt: string }) {
  return (
    <Card className="border-dashed" data-notice="evidence-deleted">
      <CardContent className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
        <ImageOff className="h-4 w-4 shrink-0" aria-hidden />
        {evidenceDeletedText(createdAt, deletedAt)}
      </CardContent>
    </Card>
  )
}

/**
 * The popup of one Evidence file of `files`, the one whose id is `openId`:
 * the image fit to the window, the video with its speed control, or the
 * notice of a deleted file; its whole caption; and the previous and the
 * next file (the buttons, or ← and →). Esc closes it. "Open page" opens the
 * file's own page.
 */
export function EvidenceViewer({ files, openId, onOpenChange }: {
  files: EvidenceFile[]
  openId: string | null
  onOpenChange: (id: string | null) => void
}) {
  const slug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const index = files.findIndex(f => f.id === openId)
  const file = index >= 0 ? files[index] : null
  const move = (step: number) => {
    const next = files[index + step]
    if (next) onOpenChange(next.id)
  }
  const keys = (e: KeyboardEvent) => {
    // A video's own controls take the arrows to seek.
    if ((e.target as HTMLElement).closest('video')) return
    if (e.key === 'ArrowLeft') move(-1)
    else if (e.key === 'ArrowRight') move(1)
    else return
    e.preventDefault()
  }
  return (
    <Dialog open={file !== null} onOpenChange={open => !open && onOpenChange(null)}>
      {file && (
        <DialogContent
          onKeyDown={keys}
          className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-y-auto sm:max-w-[min(72rem,calc(100%-2rem))]"
          data-evidence-viewer={file.id}
        >
          <DialogHeader className="pr-8">
            <DialogTitle className="whitespace-pre-wrap [overflow-wrap:anywhere]">{evidenceTitle(file)}</DialogTitle>
            <DialogDescription data-position>{index + 1} of {files.length}</DialogDescription>
          </DialogHeader>
          <div className="flex min-h-0 justify-center">
            {file.deleted_at
              ? <EvidenceDeletedNotice createdAt={file.created_at} deletedAt={file.deleted_at} />
              : isVideo(file)
                ? <EvidencePlayer key={file.id} src={evidenceBytesPath(slug, file.id)} title={evidenceTitle(file)} className="max-h-[calc(100dvh-14rem)]" />
                : <img key={file.id} src={evidenceBytesPath(slug, file.id)} alt={evidenceTitle(file)} className="max-h-[calc(100dvh-12rem)] max-w-full border object-contain" data-evidence="image" />}
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2">
            {/* A button at the end stays focusable, so the focus stays in the popup for the arrows. */}
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" onClick={() => move(-1)} disabled={index === 0} focusableWhenDisabled
                className="data-disabled:opacity-50" aria-label="Previous Evidence file">
                <ChevronLeft aria-hidden /> Previous
              </Button>
              <Button variant="outline" size="sm" onClick={() => move(1)} disabled={index === files.length - 1} focusableWhenDisabled
                className="data-disabled:opacity-50" aria-label="Next Evidence file">
                Next <ChevronRight aria-hidden />
              </Button>
            </div>
            <Link href={evidencePath(slug, file.id)} className={buttonVariants({ variant: 'outline', size: 'sm' })} data-open-page>Open page</Link>
          </div>
        </DialogContent>
      )}
    </Dialog>
  )
}
