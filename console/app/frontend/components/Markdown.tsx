import { useMemo, type MouseEvent, type ReactNode } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'

/** A link opens in a new tab, and the page it opens gets no hold on this one. */
function Link({ href, children, onClick }: { href?: string; children?: ReactNode; onClick?: (e: MouseEvent<HTMLAnchorElement>) => void }) {
  return <a href={href} target="_blank" rel="noopener noreferrer" onClick={onClick} className="text-primary underline underline-offset-2">{children}</a>
}

const COMPONENTS: Components = {
  a: ({ href, children }) => <Link href={href}>{children}</Link>,
  // An image is shown as a link to its address, so a note can't make its
  // readers' browsers fetch an outside address.
  img: ({ src, alt }) => <Link href={typeof src === 'string' ? src : undefined}>{alt || src}</Link>,
  h1: ({ children }) => <h1 className="mt-3 mb-2 text-lg font-semibold first:mt-0">{children}</h1>,
  h2: ({ children }) => <h2 className="mt-3 mb-2 text-base font-semibold first:mt-0">{children}</h2>,
  h3: ({ children }) => <h3 className="mt-3 mb-1.5 text-sm font-semibold first:mt-0">{children}</h3>,
  h4: ({ children }) => <h4 className="mt-2 mb-1 text-[13px] font-semibold first:mt-0">{children}</h4>,
  h5: ({ children }) => <h5 className="mt-2 mb-1 text-[13px] font-medium first:mt-0">{children}</h5>,
  h6: ({ children }) => <h6 className="mt-2 mb-1 text-[13px] font-medium text-muted-foreground first:mt-0">{children}</h6>,
  p: ({ children }) => <p className="my-2 first:mt-0 last:mb-0">{children}</p>,
  ul: ({ children }) => <ul className="my-2 list-disc pl-5">{children}</ul>,
  ol: ({ children }) => <ol className="my-2 list-decimal pl-5">{children}</ol>,
  li: ({ children }) => <li className="my-0.5">{children}</li>,
  blockquote: ({ children }) => <blockquote className="my-2 border-l-2 pl-3 text-muted-foreground">{children}</blockquote>,
  hr: () => <hr className="my-3 border-border" />,
  pre: ({ children }) => <pre className="my-2 overflow-x-auto bg-muted p-2 font-mono text-xs [&>code]:bg-transparent [&>code]:p-0">{children}</pre>,
  code: ({ children }) => <code className="bg-muted px-1 py-0.5 font-mono text-xs">{children}</code>,
  table: ({ children }) => <div className="my-2 overflow-x-auto"><table className="w-full border-collapse text-xs">{children}</table></div>,
  // A column's alignment marks (`---:`, `:---:`) come as the cell's style.
  th: ({ children, style }) => <th style={style} className="border px-2 py-1 text-left font-medium">{children}</th>,
  td: ({ children, style }) => <td style={style} className="border px-2 py-1">{children}</td>,
}

/**
 * Markdown, as a Text block shows it: GitHub's Markdown (tables, task lists,
 * strikethrough, autolinks). Raw HTML is not drawn and runs nothing, an
 * image is shown as a link, and a link opens in a new tab. `onLinkClick`
 * sees each click on a link first, and can prevent the new tab.
 */
export function Markdown({ text, className, onLinkClick }: {
  text: string
  className?: string
  onLinkClick?: (href: string | undefined, e: MouseEvent<HTMLAnchorElement>) => void
}) {
  const components = useMemo<Components>(() => onLinkClick
    ? { ...COMPONENTS, a: ({ href, children }) => <Link href={href} onClick={e => onLinkClick(href, e)}>{children}</Link> }
    : COMPONENTS, [onLinkClick])
  return (
    <div className={cn('text-[13px] leading-relaxed break-words', className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={components}>{text}</ReactMarkdown>
    </div>
  )
}
