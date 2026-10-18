# frozen_string_literal: true

require "test_helper"

class DashboardChartTest < ActiveSupport::TestCase
  setup do
    @dashboard = create(:dashboard)
  end

  test "a chart's Project must belong to the Dashboard's Organization" do
    foreign = create(:project, organization: create(:organization))
    chart = build(:dashboard_chart, dashboard: @dashboard, project: foreign)

    assert_not chart.valid?
    assert_includes chart.errors[:project], "must belong to the Dashboard's Organization"
  end

  test "a chart must fit the grid and draw as one of the vizzes" do
    chart = build(:dashboard_chart, dashboard: @dashboard, viz: "pie", x: 8, w: 6, h: 0)

    assert_not chart.valid?
    assert chart.errors[:viz].any?
    assert chart.errors[:w].any?
    assert chart.errors[:h].any?
  end

  test "options keep stacked, unit and decimals, read from form values, and drop the rest" do
    chart = build(:dashboard_chart, dashboard: @dashboard, options: { "stacked" => "false", "unit" => " ms ", "decimals" => "2", "color" => "red" })

    assert chart.valid?
    assert_equal({ "stacked" => false, "unit" => "ms", "decimals" => 2 }, chart.options)
  end

  test "decimals must be a whole number from 0 to 6" do
    [ "two", "7", "-1" ].each do |decimals|
      chart = build(:dashboard_chart, dashboard: @dashboard, options: { decimals: decimals })

      assert_not chart.valid?, decimals
      assert chart.errors[:options].any?, decimals
    end
  end

  test "a Dashboard's default range is a span and its auto-refresh off, 1m or 5m" do
    assert build(:dashboard, default_range: "7d", auto_refresh: "5m").valid?
    dashboard = build(:dashboard, default_range: "last week", auto_refresh: "10s")

    assert_not dashboard.valid?
    assert dashboard.errors[:default_range].any?
    assert dashboard.errors[:auto_refresh].any?
  end

  test "deleting a Project deletes its charts" do
    chart = create(:dashboard_chart, dashboard: @dashboard)

    chart.project.destroy

    assert_not DashboardChart.exists?(chart.id)
    assert Dashboard.exists?(@dashboard.id)
  end
end
