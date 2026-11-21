# frozen_string_literal: true

# The starter 'Factory' Dashboard: one Dashboard on the Agent Project with a
# Dashboard filter for the Project and four Charts from the `closed` run
# events, which carry each Run's totals (event_store/storage/runs.go):
# tokens for each merged PR, cycle time, acceptance rate and interventions
# for each Run.
#
# Console makes it one time for an Organization, when it first sees the
# Organization's Factory settings: a save in the Factory section, a read of
# the section that finds settings (a save with the CLI or the API does not
# pass Console), or the first read of the Runs page that finds a Run. The
# Organization keeps the time (factory_dashboard_made_at), so a Dashboard
# that a member deletes is not made again.
class FactoryDashboard
  NAME = "Factory"
  DEFAULT_RANGE = "30d"
  FILTER = "project"

  # A Run's closed event, of the Projects that the filter picks (all with no value).
  CLOSED = 'event.kind == "closed" and factory.project == $project'
  MERGED = 'run.close_reason == "pr_merged"'
  DECLINED = 'run.close_reason == "pr_declined"'
  TOKENS = %w[input output cache_read cache_write].map { |kind| "totals.tokens.#{kind}" }.join(" + ")

  # title, Query, options. Each Query has one number column by a time bin.
  CHARTS = [
    [ "Tokens for each merged PR",
      "where #{CLOSED} and #{MERGED}\n| summarize tokens = avg(#{TOKENS}) by bin_auto(time)",
      { "unit" => "tokens", "decimals" => 0 } ],
    [ "Cycle time of merged PRs",
      "where #{CLOSED} and #{MERGED}\n" \
      "| summarize cycle_hours = avg(totals.seconds_to_pr_opened + totals.seconds_from_pr_opened_to_outcome) / 3600 " \
      "by bin_auto(time)",
      { "unit" => "h", "decimals" => 1 } ],
    [ "Acceptance rate",
      "where #{CLOSED} and (#{MERGED} or #{DECLINED})\n" \
      "| summarize acceptance = 100 * countif(#{MERGED}) / (countif(#{MERGED}) + countif(#{DECLINED})) by bin_auto(time)",
      { "unit" => "%", "decimals" => 1 } ],
    [ "Interventions for each Run",
      "where #{CLOSED}\n| summarize interventions = avg(totals.interventions.total) by bin_auto(time)",
      { "decimals" => 2 } ]
  ].freeze

  # Makes the Dashboard for the organization, by user, unless it made one
  # before or has no Agent Project yet (a later first sight makes it). Never
  # raises: the page that calls it does not fail for a Dashboard.
  def self.make_once(organization:, user:)
    new(organization, user).make_once
  rescue StandardError => e
    Rails.logger.error("The starter Factory Dashboard was not made: #{e.class}: #{e.message}")
    nil
  end

  def initialize(organization, user)
    @organization = organization
    @user = user
  end

  def make_once
    project = @organization.agent_project
    return if project.nil? || @organization.factory_dashboard_made_at

    Dashboard.transaction do
      # One update claims it, so two reads at the same time make one Dashboard.
      claimed = Organization.where(id: @organization.id, factory_dashboard_made_at: nil)
        .update_all(factory_dashboard_made_at: Time.current)
      make(project) if claimed == 1
    end
  end

  private

  def make(project)
    dashboard = @organization.dashboards.create!(
      name: NAME, default_range: DEFAULT_RANGE, created_by: @user, filters: [ project_filter(project) ],
      description: "What the factory costs, how fast it is and how often people take its PRs.",
    )
    CHARTS.each_with_index do |(title, query, options), i|
      dashboard.charts.create!(project: project, title: title, query: query, viz: "line", options: options,
                               x: (i % 2) * 6, y: (i / 2) * 4, w: 6, h: 4)
    end
    dashboard
  end

  # A select of the Projects that the run events name.
  def project_filter(project)
    { "name" => FILTER, "label" => "Project", "kind" => "select", "multiple" => true,
      "options" => { "from" => "field", "project_id" => project.id, "field" => "factory.project" } }
  end
end
