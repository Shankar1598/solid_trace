# frozen_string_literal: true

module McpTools
  # Makes a Project in an Organization with its first key, as the New Project
  # page does. Any member can.
  class CreateProject < Base
    include ProjectKeyTool

    description "Makes a Project in an Organization, with its first key so SDKs and shippers can send to it at once. " \
                "Its slug comes from its name. get_project_setup gives the DSN, Log ingest URL and key to send with."
    writes!
    takes_organization
    argument :name, { type: "string", description: "The Project's name, such as Checkout." }, required: true
    argument :description, {
      type: "string",
      description: "What the Project holds, such as the checkout service's errors and Logs, so agents can tell which Project to query.",
    }
    argument :log_retention_days, {
      type: "integer",
      description: "How many days back its Logs are kept. The instance default when left out.",
    }

    def run
      # Project refuses a name whose slug no page could be reached at.
      made = organization.projects.build_with_key(name: arguments[:name].to_s.strip, description: arguments[:description],
        log_retention_days: arguments[:log_retention_days])
      made.save!
      json({ organization: organization.slug, project: ProjectOptionSerializer.new(made).as_json,
             key: key_json(made.project_keys.sole) }, link: links.project(made))
    end
  end
end
