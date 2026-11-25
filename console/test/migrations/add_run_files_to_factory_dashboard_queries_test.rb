# frozen_string_literal: true

require "test_helper"
require Rails.root.join("db/migrate/20261125100000_add_run_files_to_factory_dashboard_queries")

class AddRunFilesToFactoryDashboardQueriesTest < ActiveSupport::TestCase
  setup do
    @organization = create(:organization)
    @agent_project = create(:project, organization: @organization, name: "Agent sessions", agent: true)
    @dashboard = create(:dashboard, organization: @organization, name: "Factory")
  end

  test "each Chart with a Query as Console made it gets the Query of the file" do
    charts = AddRunFilesToFactoryDashboardQueries::V0_23_QUERIES.map do |query|
      create(:dashboard_chart, dashboard: @dashboard, project: @agent_project, query: query)
    end

    migrate

    assert_equal FactoryDashboard::CHARTS.map { |_title, query, _options| query }, charts.map { |chart| chart.reload.query }
  end

  test "a Query that a person changed stays, and the migration names its Chart" do
    edited = AddRunFilesToFactoryDashboardQueries::V0_23_QUERIES.first.sub("avg(used)", "max(used)")
    chart = create(:dashboard_chart, dashboard: @dashboard, project: @agent_project, title: "Tokens for each merged PR", query: edited)

    said = migrate

    assert_equal edited, chart.reload.query
    assert_match "chart #{chart.id} (Tokens for each merged PR) of Dashboard #{@dashboard.id}", said
  end

  test "a Chart on another Project, or already changed, stays" do
    old = AddRunFilesToFactoryDashboardQueries::V0_23_QUERIES.first
    elsewhere = create(:dashboard_chart, dashboard: @dashboard, project: create(:project, organization: @organization), query: old)
    current = FactoryDashboard::CHARTS.first[1]
    done = create(:dashboard_chart, dashboard: @dashboard, project: @agent_project, title: FactoryDashboard::CHARTS.first[0], query: current)

    said = migrate

    assert_equal old, elsewhere.reload.query
    assert_equal current, done.reload.query
    assert_no_match "chart #{done.id}", said
  end

  private

  def migrate
    out = StringIO.new
    original, $stdout = $stdout, out
    AddRunFilesToFactoryDashboardQueries.new.migrate(:up)
    out.string
  ensure
    $stdout = original
  end
end
