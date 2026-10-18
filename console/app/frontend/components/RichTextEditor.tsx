import { useEditor, EditorContent } from '@tiptap/react'
import { useEffect, useRef } from 'react'
import StarterKit from '@tiptap/starter-kit'
import Link from '@tiptap/extension-link'
import Placeholder from '@tiptap/extension-placeholder'
import { Bold, Italic, List, ListOrdered, Link as LinkIcon, Code } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

interface RichTextEditorProps {
  value: string
  onChange: (value: string) => void
  onKeyDown?: (e: React.KeyboardEvent) => void
  placeholder?: string
  className?: string
}

export default function RichTextEditor({ value, onChange, onKeyDown, placeholder, className }: RichTextEditorProps) {
  const editor = useEditor({
    extensions: [
      StarterKit,
      Link.configure({
        openOnClick: false,
      }),
      Placeholder.configure({
        placeholder: placeholder || 'Write something…',
      }),
    ],
    content: value,
    editorProps: {
      attributes: {
        class: cn(
          "min-h-[150px] w-full bg-background px-3 py-2 text-sm focus-visible:outline-none",
          className
        ),
      },
      handleKeyDown: (_, event) => {
        if (onKeyDown) {
          onKeyDown(event as unknown as React.KeyboardEvent)
        }
        return false
      },
    },
    onUpdate: ({ editor }) => {
      onChange(editor.getHTML())
    },
  })

  // Cleared after a post, the box keeps focus for the next comment; it doesn't take focus when the page opens.
  const previous = useRef(value)
  useEffect(() => {
    if (!editor) return
    const was = previous.current
    previous.current = value

    if (value === '') {
      editor.commands.clearContent()
      if (was !== '') editor.commands.focus()
      return
    }

    if (value !== editor.getHTML()) {
      editor.commands.setContent(value)
    }
  }, [value, editor])

  if (!editor) {
    return null
  }

  return (
    // The toolbar and the text are one control, framed once; the frame shows focus.
    <div className="border border-input focus-within:border-ring">
      <div className="flex items-center gap-1 border-b bg-muted/40 p-1">
        <Button
          variant="ghost"
          size="icon"
          onClick={() => editor.chain().focus().toggleBold().run()}
          aria-label="Bold"
          title="Bold"
          aria-pressed={editor.isActive('bold')}
          className={cn("h-8 w-8", editor.isActive('bold') && "bg-accent")}
          type="button"
        >
          <Bold className="h-4 w-4" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => editor.chain().focus().toggleItalic().run()}
          aria-label="Italic"
          title="Italic"
          aria-pressed={editor.isActive('italic')}
          className={cn("h-8 w-8", editor.isActive('italic') && "bg-accent")}
          type="button"
        >
          <Italic className="h-4 w-4" />
        </Button>
        <div className="w-px h-4 bg-border mx-1" />
        <Button
          variant="ghost"
          size="icon"
          onClick={() => editor.chain().focus().toggleBulletList().run()}
          aria-label="Bulleted list"
          title="Bulleted list"
          aria-pressed={editor.isActive('bulletList')}
          className={cn("h-8 w-8", editor.isActive('bulletList') && "bg-accent")}
          type="button"
        >
          <List className="h-4 w-4" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => editor.chain().focus().toggleOrderedList().run()}
          aria-label="Numbered list"
          title="Numbered list"
          aria-pressed={editor.isActive('orderedList')}
          className={cn("h-8 w-8", editor.isActive('orderedList') && "bg-accent")}
          type="button"
        >
          <ListOrdered className="h-4 w-4" />
        </Button>
        <div className="w-px h-4 bg-border mx-1" />
        <Button
          variant="ghost"
          size="icon"
          onClick={() => {
            const previousUrl = editor.getAttributes('link').href
            const url = window.prompt('URL', previousUrl)
            if (url === null) return
            if (url === '') {
              editor.chain().focus().extendMarkRange('link').unsetLink().run()
              return
            }
            editor.chain().focus().extendMarkRange('link').setLink({ href: url }).run()
          }}
          aria-label="Link"
          title="Link"
          aria-pressed={editor.isActive('link')}
          className={cn("h-8 w-8", editor.isActive('link') && "bg-accent")}
          type="button"
        >
          <LinkIcon className="h-4 w-4" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => editor.chain().focus().toggleCodeBlock().run()}
          aria-label="Code block"
          title="Code block"
          aria-pressed={editor.isActive('codeBlock')}
          className={cn("h-8 w-8", editor.isActive('codeBlock') && "bg-accent")}
          type="button"
        >
          <Code className="h-4 w-4" />
        </Button>
      </div>
      <EditorContent editor={editor} />
    </div>
  )
}
