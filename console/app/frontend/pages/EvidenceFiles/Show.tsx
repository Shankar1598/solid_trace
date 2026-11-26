import { Link, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { ImageOff } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardContent } from '@/components/ui/card'
import { Segmented } from '@/components/ui/segmented'
import { agentSessionPath } from '@/lib/agentSessions'
import { evidenceBytesPath, evidenceDeletedText, evidenceTitle, isVideo, readSpeed, saveSpeed, SPEEDS, type EvidenceFile, type Speed } from '@/lib/evidenceFiles'
import { bytesText } from '@/lib/overview'
import { browserStorage } from '@/lib/recentQueries'
import { sessionRecordPath, type SessionReviewRecord } from '@/lib/reviewRecords'
import type { SharedProps } from '@/types'

type EvidenceFileProps = EvidenceFile & {
  /** The Review record of the file's Agent session, null for none. */
  review_record: SessionReviewRecord | null
  /** False when EventStore could not say. */
  review_record_known: boolean
  list_path: string
}

const SPEED_OPTIONS = SPEEDS.map(speed => ({ value: speed, label: `${speed}x` }))

/**
 * One Evidence file: the screenshot, or the video with its speed buttons,
 * then its caption, who uploaded it and when, and its Agent session and
 * Review record. The caption is an agent's text, so it shows as text.
 */
export default function EvidenceFileShow({ evidence_file: file }: { evidence_file: EvidenceFileProps }) {
  const slug = usePage<SharedProps>().props.current_org?.slug ?? ''
  const title = evidenceTitle(file)
  const src = evidenceBytesPath(slug, file.id)
  return (
    <DashboardLayout>
      <PageHeader
        breadcrumbs={[{ label: 'Evidence files', href: file.list_path }, { label: title }]}
        title={<span className="break-words" data-title>{title}</span>}
        tabTitle={title}
      />
      <div className="space-y-4">
        {file.deleted_at
          ? (
            <Card className="border-dashed" data-notice="evidence-deleted">
              <CardContent className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
                <ImageOff className="h-4 w-4 shrink-0" aria-hidden />
                {evidenceDeletedText(file.created_at, file.deleted_at)}
              </CardContent>
            </Card>
          )
          : isVideo(file)
            ? <Player src={src} title={title} />
            : <img src={src} alt={title} className="max-h-[75vh] max-w-full border object-contain" data-evidence="image" />}
        <Facts file={file} slug={slug} />
      </div>
    </DashboardLayout>
  )
}

/** The video, with speed buttons that keep the viewer's last speed. */
function Player({ src, title }: { src: string; title: string }) {
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
      <video ref={video} src={src} controls preload="metadata" aria-label={title} className="max-h-[75vh] max-w-full border bg-black" data-evidence="video" />
      <Segmented label="Speed" value={speed} options={SPEED_OPTIONS} onChange={change} />
    </div>
  )
}

function Facts({ file, slug }: { file: EvidenceFileProps; slug: string }) {
  const none = (text: string) => <span className="text-muted-foreground">{text}</span>
  const facts: [string, ReactNode][] = [
    ['Caption', file.caption ? <span className="whitespace-pre-wrap">{file.caption}</span> : none('No caption')],
    ['Type', <span className="font-mono text-xs">{file.content_type}</span>],
    ['Size', <span className="font-mono text-xs">{bytesText(file.byte_size)}</span>],
    ['Uploaded by', file.uploaded_by ?? none('Deleted user')],
    ['Uploaded', <time dateTime={file.created_at}>{format(new Date(file.created_at), 'MMM d, yyyy HH:mm')}</time>],
    ['Agent session', <Link href={agentSessionPath(slug, file.agent_session_id)} className="break-all font-mono text-xs underline underline-offset-2">{file.agent_session_id}</Link>],
    ['Review record', !file.review_record_known ? none('Not known') : file.review_record
      ? (
        <span>
          <Link href={sessionRecordPath(slug, file.review_record)} aria-label={`Review record, round ${file.review_record.round}`} className="underline underline-offset-2" data-review-record-link>
            Round {file.review_record.round}
          </Link>
          {' '}<span className="font-mono text-xs text-muted-foreground">{file.review_record.pr || 'No PR'}</span>
        </span>
      )
      : none('None')],
  ]
  return (
    <Card data-section="evidence-file-facts">
      <CardContent className="grid gap-x-6 gap-y-2 py-4 text-sm sm:grid-cols-2 lg:grid-cols-3">
        {facts.map(([label, value]) => (
          <div key={label} className="min-w-0">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="break-words">{value}</div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}
