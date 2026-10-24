import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { ApiTokens } from './components/ApiTokens'

/** Settings → API tokens & MCP: the tokens agents sign in to the MCP server with, as you. */
export default function SettingsApiTokens() {
  return (
    <SettingsLayout>
      <PageHeader title="API tokens & MCP" description="The tokens Claude Code, Cursor and other MCP clients act as you with." />
      <ApiTokens />
    </SettingsLayout>
  )
}
