# frozen_string_literal: true

# A Project's Factory settings, which the Dispatcher keeps: the repository,
# the folder, the default base branch and the tracker routes, with the
# Project's schedules. Console reads and writes them through the
# Dispatcher's API as the signed-in user, so the Dispatcher lets each
# member read them and only an Admin save them.
#
# The Dispatcher's save replaces each value, the forge connection also.
# The section does not show the connection (the CLI sets it), so a save
# sends back the connection that it reads first.
class FactorySettings
  FIELDS = %w[repository folder base_branch].freeze
  SCHEDULE_FIELDS = %w[id name cron time_zone base_branch paused next_at].freeze
  # A Run's or a schedule's id, as Console's routes accept it.
  ID = /\A[A-Za-z0-9_-]{1,64}\z/

  # A route for a Project that has no Factory settings: the Dispatcher
  # keeps the routes in the settings.
  class NoSettings < StandardError
    def initialize
      super("Save the Factory settings before you add a route.")
    end
  end

  def initialize(user:, project:)
    @project = project
    @dispatcher = Dispatcher.new(user: user, organization: project.organization)
  end

  # The section's prop: { settings: { repository, folder, base_branch } or
  # nil when the Project has none, routes: [{ connection, match }],
  # schedules: [{ SCHEDULE_FIELDS, last_run: { id, state, fired_at, path }
  # or nil, pause_path, continue_path }] }, and for an Admin also tracker_connections: the names of the
  # Organization's tracker connections, for a new route. Or { error: } when
  # the Dispatcher does not answer as it should, such as when its own token
  # is not active.
  def section(admin: false)
    found = current
    section = { settings: found&.slice(*FIELDS), routes: routes_of(found), schedules: schedules }
    section[:tracker_connections] = tracker_connections if admin
    section
  rescue Dispatcher::Unavailable, Dispatcher::Refused => e
    { error: FactorySettings.message(e) }
  end

  # Saves values (FIELDS, each trimmed). The routes stay. Raises
  # Dispatcher::Refused or Dispatcher::Unavailable.
  def save(values)
    body = FIELDS.index_with { |name| values[name].to_s.strip }
    body["connection"] = current&.fetch("connection", "").to_s
    put(body)
  end

  # Adds the route (connection, match), each trimmed, to the Project's
  # routes. The Dispatcher refuses a pair that this Project or another
  # Project has. Raises NoSettings, Dispatcher::Refused or
  # Dispatcher::Unavailable.
  def add_route(connection, match)
    route = { "connection" => connection.to_s.strip, "match" => match.to_s.strip }
    change_routes { |routes| routes + [ route ] }
  end

  # Removes the route (connection, match) from the Project's routes.
  def remove_route(connection, match)
    route = { "connection" => connection.to_s, "match" => match.to_s }
    change_routes { |routes| routes - [ route ] }
  end

  # Pauses (action "pause") or continues (action "continue") a schedule at
  # once, and gives the schedule as the Dispatcher keeps it after the
  # change. Raises Dispatcher::Refused or Dispatcher::Unavailable.
  def change_schedule(id, action)
    @dispatcher.request(:post, "/api/v1/schedules/#{ERB::Util.url_encode(id)}/#{action}")
  end

  # The Dispatcher's error text as a sentence.
  def self.message(error)
    error.message.upcase_first
  end

  private

  def put(body)
    @dispatcher.request(:put, "/api/v1/projects/#{ERB::Util.url_encode(@project.slug)}/settings", body: body)
  end

  # Saves the Project's settings as they are, with the routes that the
  # block gives from the current routes. The Dispatcher replaces each value
  # of the settings, so the save sends back each value that it read.
  def change_routes
    found = current
    raise NoSettings unless found

    body = (FIELDS + [ "connection" ]).index_with { |name| found[name].to_s }
    body["routes"] = yield(routes_of(found))
    put(body)
  end

  # The Project's settings in the Dispatcher's list of the Organization's
  # settings, or nil. The list has only the Projects that have settings.
  # Orphaned settings keep the slug of a deleted Project, which a new
  # Project can have now, so they are never this Project's.
  def current
    list = @dispatcher.request(:get, "/api/v1/projects")
    projects = list_in(list, "projects")
    projects.find do |settings|
      settings.is_a?(Hash) && settings["project"] == @project.slug && !settings["orphaned"]
    end
  end

  def routes_of(settings)
    list_in(settings, "routes").filter_map do |route|
      { "connection" => route["connection"].to_s, "match" => route["match"].to_s } if route.is_a?(Hash)
    end
  end

  def schedules
    answer = @dispatcher.request(:get, "/api/v1/schedules?project=#{ERB::Util.url_encode(@project.slug)}")
    list_in(answer, "schedules").filter_map do |schedule|
      next unless schedule.is_a?(Hash) && schedule["id"].to_s.match?(ID)

      id = schedule["id"].to_s
      schedule.slice(*SCHEDULE_FIELDS).merge(
        "last_run" => last_run(schedule["last_run"]),
        "pause_path" => paths.pause_project_factory_schedule_path(@project, id, org_slug: org_slug),
        "continue_path" => paths.continue_project_factory_schedule_path(@project, id, org_slug: org_slug),
      )
    end
  end

  # A schedule's last Run with the path of its Run page, or nil.
  def last_run(run)
    return nil unless run.is_a?(Hash) && run["id"].to_s.match?(ID)

    run.slice("id", "state", "fired_at").merge("path" => paths.run_path(run["id"].to_s, org_slug: org_slug))
  end

  def paths
    Rails.application.routes.url_helpers
  end

  def org_slug
    @project.organization.slug
  end

  def tracker_connections
    list_in(@dispatcher.request(:get, "/api/v1/connections"), "connections").filter_map do |connection|
      connection["name"].to_s if connection.is_a?(Hash) && connection["tracker"].is_a?(Hash)
    end
  end

  def list_in(answer, key)
    answer.is_a?(Hash) && answer[key].is_a?(Array) ? answer[key] : []
  end
end
