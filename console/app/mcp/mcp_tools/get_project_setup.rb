# frozen_string_literal: true

module McpTools
  # What a Project's SDKs and shippers send with: the DSN the Project page
  # shows and the Log ingest URL and key the Logs page's snippets use, from
  # its first key, with a curl example and every key.
  class GetProjectSetup < Base
    include ProjectKeyTool

    description "How to send a Project its Events and Logs: the DSN a Sentry SDK takes, the Log ingest URL and key " \
                "that shippers and curl send JSON Logs to as a Bearer token, a curl example, and the Project's keys " \
                "with their ids. The DSN and Log key are the first key's, as the Project and Logs pages show them."
    read_only!
    takes_project

    def run
      keys = project.project_keys.order(:id).to_a
      first = keys.first
      logs_url = project.logs_url(ingest_url)

      setup = {
        organization: organization.slug,
        project: project.slug,
        dsn: first&.dsn(ingest_url),
        logs: { url: logs_url, key: first&.public_key },
        curl: first && curl(logs_url, first.public_key),
        keys: keys.map { |key| key_json(key) },
      }
      setup[:note] = "This Project has no key. create_key makes one." unless first

      json(setup, link: links.project(project))
    end

    private

    # Sends one Log, as the Logs page's curl snippet does.
    def curl(logs_url, key)
      [
        "curl -X POST '#{logs_url}' \\",
        "  -H 'Authorization: Bearer #{key}' \\",
        "  -H 'Content-Type: application/json' \\",
        %(  -d '{"message": "Hello from curl", "level": "info", "service": "api"}')
      ].join("\n")
    end
  end
end
