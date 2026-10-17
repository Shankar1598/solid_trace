# frozen_string_literal: true

module McpTools
  # Deletes a Project's key, so nothing sends with it. For the Organization's
  # admins.
  class DeleteKey < Base
    include ProjectKeyTool

    description "Deletes a Project's key, so SDKs and shippers using it can no longer send. Only an Organization admin can."
    destructive!
    takes_key

    def run
      authorize!(key, :destroy?, "delete a Project's keys")
      key.destroy!

      text("Deleted key #{key.id} of #{project.slug}.", link: links.project(project))
    end
  end
end
