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

  test "options keep at most 5 threshold lines in the order given, each a value with a label and a severity when given" do
    chart = build(:dashboard_chart, dashboard: @dashboard, viz: "area", options: { "thresholds" => [
      { "value" => "100", "label" => " busy ", "severity" => "critical", "color" => "red" }, { value: 0.5, label: "", severity: "" }
    ] })

    assert chart.valid?, chart.errors.full_messages.to_sentence
    assert_equal({ "thresholds" => [ { "value" => 100.0, "label" => "busy", "severity" => "critical" }, { "value" => 0.5 } ] }, chart.options)

    chart.options = { "unit" => "h", "thresholds" => [] }
    assert chart.valid?
    assert_equal({ "unit" => "h" }, chart.options)
  end

  test "threshold lines are for line and area charts, at most 5, each a number with a short label and warn or critical" do
    refused = lambda do |viz, thresholds|
      chart = build(:dashboard_chart, dashboard: @dashboard, viz: viz, options: { thresholds: thresholds })
      assert_not chart.valid?
      chart.errors[:options]
    end

    %w[bar stat table].each { |viz| assert_equal [ "thresholds are for line and area charts" ], refused.(viz, [ { value: 1 } ]) }
    assert_equal [ "thresholds are at most 5" ], refused.("line", Array.new(6) { |i| { value: i } })
    assert_equal [ "thresholds must be a list" ], refused.("line", { value: 1 })
    assert_equal [
      "thresholds[0].value must be a number",
      "thresholds[1].value must be a number",
      "thresholds[2].label is too long (at most 40 characters)",
      "thresholds[3].severity must be warn or critical",
      "thresholds[4] must be an object"
    ], refused.("line", [ { value: "high" }, { label: "no value" }, { value: 1, label: "x" * 41 }, { value: 1, severity: "info" }, 5 ])
  end

  test "a Text block is Markdown with no Project, no Query and no title needed" do
    block = build(:dashboard_chart, :text, dashboard: @dashboard, text: "## Runbook\n\nSee [the wiki](https://wiki.example).")

    assert block.valid?, block.errors.full_messages.to_sentence
    block.save!
    assert_equal [ "text", nil, nil, nil ], block.reload.values_at(:viz, :project_id, :query, :title)
  end

  test "a Text block needs its text, at most 10,000 characters, and refuses a Project, a Query and options" do
    refused = lambda do |**fields|
      block = build(:dashboard_chart, :text, dashboard: @dashboard, **fields)
      assert_not block.valid?
      block.errors.to_hash.transform_values(&:sole)
    end

    assert_equal({ text: "can't be blank" }, refused.(text: " "))
    assert_equal({ text: "is too long (maximum is 10000 characters)" }, refused.(text: "x" * 10_001))
    assert_equal({ project: "must be empty for a Text block", query: "must be empty for a Text block" },
      refused.(project: create(:project, organization: @dashboard.organization), query: "summarize count()"))
    assert_equal({ options: "are not for a Text block" }, refused.(options: { unit: "ms" }))
  end

  test "a Chart other than a Text block needs a Project, a Query and a title, and takes no text" do
    chart = build(:dashboard_chart, dashboard: @dashboard, project: nil, query: "", title: "", text: "a note")

    assert_not chart.valid?
    assert_equal({ project: "must exist", query: "can't be blank", title: "can't be blank", text: "is only for a Text block" },
      chart.errors.to_hash.transform_values(&:sole))
  end

  test "a Chart can not become a Text block, nor a Text block a Chart" do
    chart = create(:dashboard_chart, dashboard: @dashboard)
    block = create(:dashboard_chart, :text, dashboard: @dashboard)

    chart.assign_attributes(viz: "text", project: nil, query: nil, text: "a note")
    block.assign_attributes(viz: "line", project: create(:project, organization: @dashboard.organization), query: "summarize count()", text: nil)

    [ chart, block ].each do |record|
      assert_not record.valid?
      assert_equal [ "can not change between text and a Chart's viz: delete it and add a new one" ], record.errors[:viz]
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

  test "a chart runs a Query unless it is given SQL, and a Text block has no language" do
    assert_equal "query", create(:dashboard_chart, dashboard: @dashboard).language
    assert_equal "sql", create(:dashboard_chart, :sql, dashboard: @dashboard).language
    assert_nil create(:dashboard_chart, :text, dashboard: @dashboard, language: "sql").language

    chart = build(:dashboard_chart, dashboard: @dashboard, language: "python")
    assert_not chart.valid?
    assert chart.errors[:language].any?
  end

  test "a SQL Chart is saved only while SQL queries are on, and while they are off a saved one keeps its SQL and can move" do
    chart = create(:dashboard_chart, :sql, dashboard: @dashboard)
    InstanceSetting.current.update!(sql_queries_enabled: false)

    added = build(:dashboard_chart, :sql, dashboard: @dashboard)
    assert_not added.valid?
    assert_includes added.errors[:base], LogMonitor::SQL_QUERIES_OFF
    chart.query = "SELECT 1"
    assert_not chart.valid?
    assert_includes chart.errors[:base], LogMonitor::SQL_QUERIES_OFF

    chart.reload
    assert chart.update(x: 6, title: "Levels")
    assert create(:dashboard_chart, dashboard: @dashboard).valid?
    query_chart = create(:dashboard_chart, dashboard: @dashboard)
    assert_not query_chart.update(language: "sql", query: "SELECT 1")
  end
end
