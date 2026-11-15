# frozen_string_literal: true

# A Project's Factory settings, which the Dispatcher keeps: the repository,
# the folder and the default base branch. Console reads and writes them
# through the Dispatcher's API as the signed-in user, so the Dispatcher
# lets each member read them and only an Admin save them.
#
# The Dispatcher's save replaces each value, the forge connection also.
# The section does not show the connection (the CLI sets it), so a save
# sends back the connection that it reads first.
class FactorySettings
  FIELDS = %w[repository folder base_branch].freeze

  def initialize(user:, project:)
    @project = project
    @dispatcher = Dispatcher.new(user: user, organization: project.organization)
  end

  # The section's prop: { settings: { repository, folder, base_branch } or
  # nil when the Project has none }, or { error: } when the Dispatcher does
  # not answer as it should, such as when its own token is not active.
  def section
    found = current
    { settings: found&.slice(*FIELDS) }
  rescue Dispatcher::Unavailable, Dispatcher::Refused => e
    { error: FactorySettings.message(e) }
  end

  # Saves values (FIELDS, each trimmed). Raises Dispatcher::Refused or
  # Dispatcher::Unavailable.
  def save(values)
    body = FIELDS.index_with { |name| values[name].to_s.strip }
    body["connection"] = current&.fetch("connection", "").to_s
    @dispatcher.request(:put, "/api/v1/projects/#{ERB::Util.url_encode(@project.slug)}/settings", body: body)
  end

  # The Dispatcher's error text as a sentence.
  def self.message(error)
    error.message.upcase_first
  end

  private

  # The Project's settings in the Dispatcher's list of the Organization's
  # settings, or nil. The list has only the Projects that have settings.
  # Orphaned settings keep the slug of a deleted Project, which a new
  # Project can have now, so they are never this Project's.
  def current
    list = @dispatcher.request(:get, "/api/v1/projects")
    projects = list.is_a?(Hash) && list["projects"].is_a?(Array) ? list["projects"] : []
    projects.find do |settings|
      settings.is_a?(Hash) && settings["project"] == @project.slug && !settings["orphaned"]
    end
  end
end
