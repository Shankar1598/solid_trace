# frozen_string_literal: true

# The engineer's notice: which repositories upload, to which Organization,
# and that each member of it can read them. The approval page and the
# device page show it for a sign-in that asks for sessions:write (as
# `solidtrace login` does), with the Organization's current list. The text
# is the same as the CLI prints after the sign-in (cli/command/login.go
# printNotice): change both together.
module CaptureNotice
  NOTHING_UPLOADS = "%{organization} has no captured repositories, so no Agent session uploads from this machine."
  UPLOADS = "Your Claude Code Agent sessions in these repositories upload in full to %{organization}, " \
            "and each member of %{organization} can read them:"
  OTHERS_DO_NOT = "Agent sessions in other repositories, or in no repository, do not upload."

  module_function

  # { lead:, repositories:, closing: } for organization. With Agent
  # sessions off, or an empty list, the lead says that nothing uploads.
  def for(organization)
    repositories = organization.agent_project&.captured_repositories || []
    if repositories.empty?
      { lead: format(NOTHING_UPLOADS, organization: organization.name), repositories: [], closing: nil }
    else
      { lead: format(UPLOADS, organization: organization.name), repositories: repositories, closing: OTHERS_DO_NOT }
    end
  end

  # The notice for organization when scopes holds sessions:write, else nil.
  def for_scopes(organization, scopes)
    self.for(organization) if scopes.any? { |scope| scope.name == "sessions:write" }
  end
end
