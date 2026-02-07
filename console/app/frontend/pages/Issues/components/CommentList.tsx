import { useForm, router } from '@inertiajs/react'
import { Card, CardHeader, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { formatDistanceToNow } from 'date-fns'
import { Comment, User } from '@/types'
import RichTextEditor from '@/components/RichTextEditor'
import { Trash } from 'lucide-react'
import { toast } from 'sonner'

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
  const handlePostComment = () => {
    if (!data.content || data.content === '<p></p>' || processing) return

    post(`/${orgSlug}/projects/${projectId}/issues/${issueId}/comments`, {
      onSuccess: () => {
        reset('content')
        toast.success('Comment posted successfully')
      },
      onError: () => toast.error('Failed to post comment'),
      preserveScroll: true,
      preserveState: true
    })
  }

  const submitComment = (e: React.FormEvent) => {
    e.preventDefault()
    handlePostComment()
  }

  const deleteComment = (commentId: number) => {
    if (confirm('Are you sure you want to delete this comment?')) {
      router.delete(`/${orgSlug}/projects/${projectId}/issues/${issueId}/comments/${commentId}`, {
        onSuccess: () => toast.success('Comment deleted successfully'),
        onError: () => toast.error('Failed to delete comment'),
        preserveScroll: true
      })
    }
  }

  return (
    <div className="space-y-6">
      <div className="space-y-4">
        {comments.map((comment) => (
          <Card key={comment.id} id={`comment-${comment.id}`} className="py-0">
            <CardHeader className="py-3 bg-muted/30 flex flex-row items-center justify-between space-y-0">
              <div className="flex items-center gap-2 text-sm">
                <span className="font-semibold">{comment.user.name}</span>
                <span className="text-muted-foreground">commented</span>
                <span className="text-muted-foreground">
                  {formatDistanceToNow(new Date(comment.created_at), { addSuffix: true })}
                </span>
              </div>
              {currentUser && currentUser.id === comment.user.id && (
                <Button
                  onClick={() => deleteComment(comment.id)}
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6 text-muted-foreground hover:text-destructive"
                >
                  <Trash className="h-4 w-4" />
                </Button>
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
            onKeyDown={(e: React.KeyboardEvent) => {
              if (e.key === 'Enter' && e.shiftKey) {
                e.preventDefault()
                handlePostComment()
              }
            }}
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
