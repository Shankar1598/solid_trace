/** The ways a page can put text on the clipboard. */
export interface ClipboardWays {
  /** navigator.clipboard.writeText, which browsers give only to secure pages (HTTPS or localhost). */
  writeText?: (text: string) => Promise<void>
  /** Selects the text in a hidden textarea and runs the copy command; says whether it copied. */
  execCopy: (text: string) => boolean
}

/**
 * Copies text, and says whether it worked. It tries the clipboard first, then
 * a hidden textarea, which also works on a page served over plain HTTP.
 */
export async function copyToClipboard(text: string, ways: ClipboardWays = browserClipboard()): Promise<boolean> {
  if (ways.writeText) {
    try {
      await ways.writeText(text)
      return true
    } catch {
      // Refused or unsupported: try the textarea.
    }
  }
  try {
    return ways.execCopy(text)
  } catch {
    return false
  }
}

/** The browser's ways: its clipboard when the page has one, and a hidden textarea. */
export function browserClipboard(): ClipboardWays {
  const clipboard = typeof navigator === 'undefined' ? undefined : navigator.clipboard
  return {
    writeText: clipboard ? (text) => clipboard.writeText(text) : undefined,
    execCopy: (text) => {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.setAttribute('readonly', '')
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      try {
        textarea.select()
        return document.execCommand('copy')
      } finally {
        textarea.remove()
      }
    },
  }
}
