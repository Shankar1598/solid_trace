import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { Markdown } from './Markdown'

const html = (text: string) => renderToStaticMarkup(createElement(Markdown, { text }))

describe('Markdown', () => {
  it('draws headings, lists, bold, code and GitHub tables', () => {
    const out = html('## Runbook\n\n- **Restart** the `worker`\n\n| step | who |\n| --- | --- |\n| 1 | on-call |')

    expect(out).toContain('>Runbook</h2>')
    expect(out).toContain('<strong>Restart</strong>')
    expect(out).toContain('>worker</code>')
    expect(out).toMatch(/<table[^>]*>.*<td[^>]*>on-call<\/td>/s)
  })

  it('opens a link in a new tab, with no opener and no referrer', () => {
    expect(html('See [the wiki](https://wiki.example/run).')).toContain(
      '<a href="https://wiki.example/run" target="_blank" rel="noopener noreferrer"',
    )
  })

  it('shows no raw HTML and runs no script', () => {
    const out = html('<script>alert(1)</script>\n\nHi <b onclick="x()">there</b>')

    expect(out).not.toContain('<script')
    expect(out).not.toContain('<b')
    expect(out).not.toContain('onclick=')
  })

  it('draws no image: it shows the image as a link to its address', () => {
    const out = html('![the graph](https://img.example/a.png)')

    expect(out).not.toContain('<img')
    expect(out).toContain('<a href="https://img.example/a.png" target="_blank" rel="noopener noreferrer"')
    expect(out).toContain('the graph')
  })

  it('drops a javascript: link address', () => {
    expect(html('[click](javascript:alert(1))')).not.toContain('javascript:')
  })
})
