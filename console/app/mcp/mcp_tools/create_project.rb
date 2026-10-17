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
    argument :log_retention_days, {
      type: "integer",
      description: "How many days back its Logs are kept. The instance default when left out.",
    }

    def run
      made = organization.projects.build_with_key(name: arguments[:name].to_s.strip, log_retention_days: arguments[:log_retention_days])
      raise ToolError, "Give the Project a name." if made.name.blank?

      # Every Project's page must be reachable at its slug.
      slug = made.name.parameterize
      raise ToolError, "Give the Project a name with Latin letters or digits. Its slug is made from them." if slug.blank?
      if slug == "new"
        raise ToolError, "A Project can't be called new, since its page would be the New Project page. Give another name."
      end
      if organization.projects.exists?(slug: slug)
        raise ToolError, "#{organization.slug} already has a Project #{slug}. Give another name, or use that one."
      end

      made.save!
      json({ organization: organization.slug, project: ProjectOptionSerializer.new(made).as_json,
             key: key_json(made.project_keys.sole) }, link: links.project(made))
    end
  end
end
