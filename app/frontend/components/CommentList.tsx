import { Link, useForm } from '@inertiajs/react'
import { Card, CardHeader, CardContent } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { formatDistanceToNow } from 'date-fns'
import { Comment, User } from '@/types'
import RichTextEditor from '@/components/RichTextEditor'
import { Trash2 } from 'lucide-react'

interface CommentListProps {
  comments: Comment[]
  currentUser: User | null
  issueId: number
  projectId: string
  orgSlug: string
}

export default function CommentList({ comments, currentUser, issueId, projectId, orgSlug }: CommentListProps) {
  const { data, setData, post, processing, reset } = useForm({
    content: ''
  })

  // We need to construct the URL manually or via a route helper if available
  // Ideally Inertia props would include the issue URL or we'd use a route helper
  const submitComment = (e: React.FormEvent) => {
    e.preventDefault()
    post(`/${orgSlug}/projects/${projectId}/issues/${issueId}/comments`, {
      onSuccess: () => reset()
    })
  }

  return (
    <div className="space-y-6">
      <div className="space-y-4">
        {comments.map((comment) => (
          <Card key={comment.id} id={`comment-${comment.id}`}>
            <CardHeader className="py-3 bg-muted/30 flex flex-row items-center justify-between space-y-0">
              <div className="flex items-center gap-2 text-sm">
                <span className="font-semibold">{comment.user.name}</span>
                <span className="text-muted-foreground">commented</span>
                <span className="text-muted-foreground">
                  {formatDistanceToNow(new Date(comment.created_at), { addSuffix: true })}
                </span>
              </div>
              {currentUser && currentUser.id === comment.user.id && (
                <Link
                  href={`/${orgSlug}/projects/${projectId}/issues/${issueId}/comments/${comment.id}`}
                  method="delete"
                  as="button"
                  preserveScroll
                  className={cn(buttonVariants({ variant: "ghost", size: "icon" }), "h-6 w-6 text-muted-foreground hover:text-destructive")}
                >
                  <Trash2 className="h-4 w-4" />
                </Link>
              )}
            </CardHeader>
            <CardContent className="py-3 prose dark:prose-invert max-w-none text-sm" dangerouslySetInnerHTML={{ __html: comment.body }} />
          </Card>
        ))}
      </div>

      <div className="pt-4 border-t">
        <h3 className="font-semibold mb-4">Add a comment</h3>
        <form onSubmit={submitComment} className="space-y-4">
          <RichTextEditor
            value={data.content}
            onChange={(val) => setData('content', val)}
            placeholder="Leave a comment..."
          />
          <div className="flex justify-end">
            <Button type="submit" disabled={processing || !data.content || data.content === '<p></p>'}>
              {processing ? 'Posting...' : 'Post Comment'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  )
}
