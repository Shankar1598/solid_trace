import { Link, usePage } from '@inertiajs/react'
import { format } from 'date-fns'
import { Images } from 'lucide-react'
import { EvidenceThumb } from '@/components/EvidenceFileList'
import DashboardLayout from '@/components/layouts/DashboardLayout'
import { PageHeader } from '@/components/PageHeader'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { agentSessionPath } from '@/lib/agentSessions'
import { evidencePath, evidenceTitle, type EvidenceFile } from '@/lib/evidenceFiles'
import { bytesText } from '@/lib/overview'
import type { SharedProps } from '@/types'

type EvidenceFilesProps = { rows: EvidenceFile[]; more: boolean }

const DESCRIPTION = 'The screenshots and videos that agents uploaded to prove their PRs, newest first.'

export default function EvidenceFilesIndex({ evidence_files: view }: { evidence_files: EvidenceFilesProps }) {
  const slug = usePage<SharedProps>().props.current_org?.slug ?? ''
  return (
    <DashboardLayout>
      <PageHeader title="Evidence files" description={DESCRIPTION} />
      {view.rows.length === 0
        ? (
          <Card className="border-dashed" data-notice="no-evidence-files">
            <CardContent className="flex flex-col items-center justify-center py-12 text-center">
              <Images className="mb-4 h-12 w-12 text-muted-foreground opacity-50" aria-hidden />
              <h2 className="text-lg font-medium">No Evidence files</h2>
              <p className="max-w-md text-sm text-muted-foreground">
                The open-pr and update-pr skills of the solidtrace plugin upload the screenshots and videos of their checks with solidtrace evidence add. They show here.
              </p>
            </CardContent>
          </Card>
        )
        : (
          <div className="space-y-2">
            <div className="overflow-x-auto border">
              <Table aria-label="Evidence files">
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-20"><span className="sr-only">Preview</span></TableHead>
                    <TableHead>Caption</TableHead>
                    <TableHead className="max-md:hidden">Type</TableHead>
                    <TableHead className="text-right max-md:hidden">Size</TableHead>
                    <TableHead className="max-md:hidden">Agent session</TableHead>
                    <TableHead className="max-md:hidden">Uploaded by</TableHead>
                    <TableHead className="max-md:hidden">Uploaded</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {view.rows.map(file => (
                    <TableRow key={file.id} data-evidence-file={file.id}>
                      <TableCell><EvidenceThumb file={file} className="h-10 w-16" /></TableCell>
                      <TableCell className="max-w-64 md:max-w-md" data-cell="caption">
                        <Link href={evidencePath(slug, file.id)} className="block truncate font-medium underline-offset-2 hover:underline" title={evidenceTitle(file)}>
                          {evidenceTitle(file)}
                        </Link>
                        {file.deleted_at && <Badge variant="neutral" data-badge="deleted">deleted</Badge>}
                        <div className="mt-1 space-y-0.5 font-mono text-xs text-muted-foreground md:hidden" data-cell="phone-facts">
                          <div className="flex gap-2"><UploadedAt file={file} /><SessionLink file={file} slug={slug} /></div>
                          <div className="truncate">{file.content_type} · {bytesText(file.byte_size)}</div>
                        </div>
                      </TableCell>
                      <TableCell className="font-mono text-xs max-md:hidden" data-cell="type">{file.content_type}</TableCell>
                      <TableCell className="text-right font-mono text-xs tabular-nums whitespace-nowrap max-md:hidden" data-cell="size">{bytesText(file.byte_size)}</TableCell>
                      <TableCell className="max-md:hidden"><SessionLink file={file} slug={slug} /></TableCell>
                      <TableCell className="text-xs max-md:hidden">{file.uploaded_by ?? <span className="text-muted-foreground">Deleted user</span>}</TableCell>
                      <TableCell className="font-mono text-xs whitespace-nowrap max-md:hidden" data-cell="uploaded"><UploadedAt file={file} /></TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            {view.more && <p className="text-xs text-muted-foreground">The {view.rows.length.toLocaleString()} newest Evidence files.</p>}
          </div>
        )}
    </DashboardLayout>
  )
}

function UploadedAt({ file }: { file: EvidenceFile }) {
  return <time dateTime={file.created_at}>{format(new Date(file.created_at), 'MMM d, HH:mm')}</time>
}

function SessionLink({ file, slug }: { file: EvidenceFile; slug: string }) {
  return (
    <Link href={agentSessionPath(slug, file.agent_session_id)} className="font-mono text-xs text-foreground underline underline-offset-2">
      {file.agent_session_id.slice(0, 8)}
    </Link>
  )
}
