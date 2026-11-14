# MCP clients sign in with OAuth, built in Console

Amends [ADR 0009](0009-mcp-server-in-console-with-api-tokens.md), whose "OAuth sign-in in v1" was deferred, and [ADR 0018](0018-passkeys-count-as-two-factor-on-the-console-urls-domain.md), which left API tokens outside Two-factor.

Console is an OAuth 2.1 authorization server for its own MCP server, as the MCP authorization specification (2026-07-28) tells. An MCP client that gets a 401 from `POST /mcp` finds the metadata (RFC 9728, RFC 8414), registers, and opens Console's approval page in the browser. The user signs in, with Two-factor where they have it, picks one Organization, and approves. The client gets an access token and a refresh token, with PKCE (S256). API tokens do not change: they stay for scripts.

The decisions:

- **Own code, no OAuth gem.** About ten small endpoints and five tables (`oauth_clients`, `oauth_grants`, `oauth_codes`, `oauth_tokens`, `oauth_pending_requests`) in an `Oauth` namespace. Doorkeeper's checks are copied as test cases, not as code.
- **CIMD and DCR, both.** A client with a published description (a Client ID Metadata Document) has an HTTPS URL as its `client_id`. Console fetches the document only for a signed-in user, with an SSRF guard (public addresses only, no redirects, 5 s, 64 KB), and keeps a copy that the token endpoint uses. The approval page marks its name "Checked: published at *host*". Open Dynamic Client Registration (RFC 7591) stays for older clients and for installs that can't reach the internet, and marks the name "Not checked".
- **The device flow (RFC 8628) only for CIMD clients.** With open DCR, any program could take a trusted name on the device page. The user always types the code, and the page shows the published host and the time and IP address of the request.
- **Opaque tokens, stored as SHA-256 digests.** `sto_` access tokens and `str_` refresh tokens, 32 random bytes each, looked up by digest as API tokens are, not JWTs. A revoke stops a token at once, and a copy of the database gives no usable secret. The one exception is the new pair kept on a used refresh token for 60 seconds, encrypted with Active Record Encryption, then cleared.
- **One Organization for each Grant.** Each approval is a Grant for one Organization, picked on the approval page, also for the same client on a second computer. Over MCP the token sees only that Organization, and the install's settings tools are not offered. One resolver, `BearerToken.authenticate`, answers for API tokens and access tokens, at `/mcp` and at introspection (RFC 7662).
- **OAuth only with an HTTPS Console URL** (the Console URL setting, else `CONSOLE_URL`), or a loopback `http://` URL for development. The issuer never comes from the request's host. Elsewhere OAuth is off: the metadata answers 404, the 401 is a plain `Bearer`, and Settings → API tokens & MCP says why. The resource is `<Console URL>/mcp`, so a change of the Console URL stops every Grant.
- **The numbers:** an access token works 1 hour; a refresh token 30 days from its issue, and each refresh gives a new one with a new 30 days, so a Grant in use does not expire and a Grant not used for 30 days does; an authorization code 60 seconds; a device code 10 minutes.
- **One refresh chain.** A refresh token works once (a conditional update). A second use within 60 seconds gets the same new pair again, so several copies of one client that refresh together stay signed in. After 60 seconds, a reuse revokes the Grant. A new pair inside the window would let a thief start a second chain that reuse detection never finds.

We chose this because a pasted API token is a long-lived secret on disk that acts in every Organization and that Two-factor does not protect, and web connectors can't use one at all.

## Considered options

- **Doorkeeper.** Its 6.0 is a release candidate, it has no RFC 9728, DCR or device flow, its device-flow gem does not work with 6.0, and its pages would be replaced by Inertia pages. One resolver for API tokens and OAuth tokens is simpler on our own tables.
- **CIMD only.** Older clients and offline installs would have only API tokens.
- **JWT access tokens.** No lookup, but no revoke at once, and SQLite already does this lookup for API tokens.
- **A Grant for all the user's Organizations.** As an API token is; but a client approved for one Organization could read another.
- **A new refresh token at each use, with no window.** Several copies of Claude Code share one stored token and can refresh at the same time; one of them would be signed out at random.

## Consequences

- Two-factor now protects MCP sign-in: the approval page is a Console page, so Require two-factor applies to it. API tokens are still made from a Session with no second factor.
- Settings → User lists the user's Grants as Signed-in clients, with Revoke. `claude mcp logout` revokes the Grant through `/oauth/revoke`. A password reset, the operator's `users:reset_two_factor` and a removal from the Organization revoke Grants.
- An install that can't reach the internet can't fetch CIMD documents. Its clients use DCR, and can't use the device flow.
- The MCP URL in a client must use the Console URL's host: a client refuses metadata that names another resource.
- Later efforts add scopes and resources to `Oauth::Scopes`; a Grant already keeps a list of resources, so the tables need no change.

Source: [.scratch/mcp-oauth/spec.md](../../.scratch/mcp-oauth/spec.md)
