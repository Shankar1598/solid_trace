# frozen_string_literal: true

module McpTools
  # What the Project setup and key tools share: the key argument, the key it
  # names in the Project, where ingest is, and a key as the tools answer it.
  # Only an Organization's admins make, rotate and delete keys
  # (ProjectKeyPolicy), as on the Project page.
  module ProjectKeyTool
    extend ActiveSupport::Concern

    KEY = { type: "integer", description: "The key's id. get_project_setup lists a Project's keys." }.freeze

    class_methods do
      # The organization, project and key arguments, which #key reads.
      def takes_key
        takes_project
        argument :key, KEY, required: true
      end
    end

    private

    # The key the key argument names, in #project.
    def key
      @key ||= project.project_keys.find(arguments[:key])
    end

    # Where SDKs and shippers reach ingest, as the Project and Logs pages say.
    def ingest_url
      EventStore.public_ingest_url(console_url)
    end

    # A key with its DSN, without its secret.
    def key_json(key)
      ProjectKeySerializer.new(key, ingest_url: ingest_url).as_json.except(:label)
    end

    # A key in its Project, as the key tools answer it.
    def key_answer(key, **more)
      json({ organization: organization.slug, project: project.slug, key: key_json(key), **more }, link: links.project(project))
    end
  end
end
