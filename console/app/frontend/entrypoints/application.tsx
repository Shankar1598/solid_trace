import { createInertiaApp } from '@inertiajs/react'
import { createRoot } from 'react-dom/client'
import '../css/globals.css'
import { ErrorBoundary } from '@sentry/react'
import { Toaster } from '../components/ui/sonner'
import { PageCrash } from '../components/PageCrash'
import { reportBrowserErrors } from '../lib/browserErrors'
import type { SharedProps } from '../types'

createInertiaApp({
  title: title => (title ? `${title} · SolidTrace` : 'SolidTrace'),
  resolve: (name) => {
    const pages = import.meta.glob('../pages/**/*.tsx', { eager: true })
    const page = pages[`../pages/${name}.tsx`]
    if (!page) {
      throw new Error(`Page not found: ${name}`)
    }
    return page
  },
  setup({ el, App, props }) {
    reportBrowserErrors((props.initialPage.props as unknown as SharedProps).browser_errors)
    // A page that throws while it renders shows PageCrash, not a blank page.
    createRoot(el).render(
      <>
        <ErrorBoundary fallback={({ error }) => <PageCrash error={error} />}>
          <App {...props} />
        </ErrorBoundary>
        <Toaster />
      </>
    )
  },
})
