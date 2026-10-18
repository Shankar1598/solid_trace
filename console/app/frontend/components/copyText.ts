import { toast } from 'sonner'

/** Copies text, and a toast says whether it worked: "<what> copied". */
export function copyText(text: string, what: string) {
  const clipboard = typeof navigator === 'undefined' ? undefined : navigator.clipboard
  if (!clipboard) {
    toast.error(`Could not copy the ${what.toLowerCase()}: this browser gives the page no clipboard.`)
    return
  }
  clipboard.writeText(text)
    .then(() => toast.success(`${what} copied`))
    .catch(() => toast.error(`Could not copy the ${what.toLowerCase()}. Allow the clipboard for this site and try again.`))
}
