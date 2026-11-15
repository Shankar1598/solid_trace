import { Copy } from 'lucide-react'
import { copyText } from '@/components/copyText'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { McpSnippet } from '@/lib/apiTokens'

/** The snippets that add the MCP server to each client, one tab each, with Copy. */
export function SnippetTabs({ snippets }: { snippets: McpSnippet[] }) {
  return (
    <Tabs defaultValue={snippets[0].id} className="gap-0">
      <TabsList variant="line">
        {snippets.map(s => <TabsTrigger key={s.id} value={s.id}>{s.label}</TabsTrigger>)}
      </TabsList>
      {snippets.map(s => (
        <TabsContent key={s.id} value={s.id} className="flex flex-col gap-2 border-t pt-3">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-muted-foreground">{s.note}</span>
            <Button variant="outline" size="sm" onClick={() => copyText(s.code, 'Snippet')} aria-label={`Copy the ${s.label} snippet`}>
              <Copy className="h-3.5 w-3.5" /> Copy
            </Button>
          </div>
          <pre className="overflow-x-auto border bg-muted/40 p-3 font-mono text-xs leading-5">{s.code}</pre>
        </TabsContent>
      ))}
    </Tabs>
  )
}
