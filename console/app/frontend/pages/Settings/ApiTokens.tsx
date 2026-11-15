import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { ApiTokens } from './components/ApiTokens'
import { McpSignIn } from './components/McpSignIn'

/**
 * Settings → API tokens & MCP: how MCP clients sign in to the MCP server as
 * you, first through the browser, then with API tokens for scripts.
 */
export default function SettingsApiTokens() {
  return (
    <SettingsLayout>
      <PageHeader title="API tokens & MCP" description="How Claude Code, Cursor and other MCP clients act as you through SolidTrace's MCP server." />
      <div className="space-y-6">
        <McpSignIn />
        <ApiTokens />
      </div>
    </SettingsLayout>
  )
}
