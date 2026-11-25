import { Button, buttonVariants } from '@/components/ui/button'

/**
 * What shows in place of a page that stopped with an error, in place of a
 * blank page. The error boundary has already sent the error to Console's
 * Sentry when it sends errors.
 */
export function PageCrash({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div role="alert" className="flex min-h-svh items-center justify-center bg-background p-4 text-foreground" data-page-crash>
      <div className="flex max-w-lg flex-col gap-3">
        <h1 className="text-lg font-semibold">This page stopped with an error</h1>
        <p className="text-sm text-muted-foreground">Reload the page to continue. Changes that you did not save are lost.</p>
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words border bg-muted/40 p-2 font-mono text-xs">{message}</pre>
        <div className="flex gap-2">
          <Button onClick={() => window.location.reload()}>Reload page</Button>
          <a href="/" className={buttonVariants({ variant: 'outline' })}>Go to start page</a>
        </div>
      </div>
    </div>
  )
}
