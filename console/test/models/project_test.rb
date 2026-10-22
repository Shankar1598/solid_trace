# frozen_string_literal: true

require "test_helper"

class ProjectTest < ActiveSupport::TestCase
  include MonitorTestHelper

  test "generates slug on create" do
    project = create(:project, name: "My Project", slug: nil)

    assert_equal "my-project", project.slug
  end

  test "a Project needs a name" do
    project = build(:project, name: "", slug: nil)

    assert_not project.valid?
    assert_equal [ "can't be blank" ], project.errors[:name]
  end

  # A Project is found by its slug in its Organization, at
  # /:org_slug/projects/:slug, so a slug no Project could be reached at is
  # refused, on the name the slug is made from.
  test "a name that gives no slug is refused" do
    [ "日本語", "Проект", "!!!" ].each do |name|
      project = build(:project, name: name, slug: nil)

      assert_not project.valid?, name
      assert_equal [ "needs Latin letters or digits, since the Project's slug is made from them" ], project.errors[:name]
    end
  end

  test "a name that gives the slug new is refused, since that is the New Project page" do
    project = build(:project, name: "New", slug: nil)

    assert_not project.valid?
    assert_equal [ "gives the slug new, which is the New Project page's address" ], project.errors[:name]
  end

  test "a name whose slug another Project of the Organization has is refused" do
    shop = create(:project, name: "Shop", slug: nil)
    project = build(:project, organization: shop.organization, name: "SHOP", slug: nil)

    assert_not project.valid?
    assert_equal [ "gives the slug shop, which another of this Organization's Projects has" ], project.errors[:name]
    assert build(:project, name: "Shop", slug: nil).valid?, "another Organization's Project may have it"
  end

  test "a Project made before the slug checks can still be saved" do
    project = create(:project, name: "Shop", slug: nil)
    project.update_column(:slug, "")

    assert project.update(log_retention_days: 7)
  end

  test "a Project's Log file order is the default when unset, and time only when empty" do
    assert build(:project, log_file_order: nil).valid?
    assert build(:project, log_file_order: []).valid?
    assert_equal [ "service", "file" ], build(:project, log_file_order: nil).log_file_order_in_force
    assert_equal [], build(:project, log_file_order: []).log_file_order_in_force
    assert_equal [ "level" ], build(:project, log_file_order: [ "level" ]).log_file_order_in_force
  end

  test "a Log file order names service, environment, level or Attributes, up to three" do
    assert build(:project, log_file_order: [ "service", "file" ]).valid?
    assert build(:project, log_file_order: [ "environment", "level", "http.route" ]).valid?
    assert build(:project, log_file_order: [ "user agent" ]).valid?, "a Query reads any Attribute name, quoted"
  end

  test "a Log file order of a fourth Field is refused" do
    project = build(:project, log_file_order: [ "service", "file", "level", "environment" ])

    assert_not project.valid?
    assert_equal [ "can have at most 3 Fields" ], project.errors[:log_file_order]
  end

  test "a Log file order naming a Field twice is refused" do
    project = build(:project, log_file_order: [ "service", "file", "service" ])

    assert_not project.valid?
    assert_equal [ "names service twice" ], project.errors[:log_file_order]
  end

  test "a Log file order can't name time, id, message, trace_id or span_id" do
    %w[time _time id message trace_id span_id].each do |field|
      project = build(:project, log_file_order: [ "service", field ])

      assert_not project.valid?, "#{field} should be refused"
      assert_equal [ "can't name #{field}: only service, environment, level and Attributes sort a Log file" ],
        project.errors[:log_file_order]
    end
  end

  test "a Log file order of a name no Log can have as an Attribute is refused" do
    [ "", "a\u0000b", "a" * 257 ].each do |name|
      project = build(:project, log_file_order: [ name ])

      assert_not project.valid?, "#{name.inspect} should be refused"
      assert project.errors[:log_file_order].present?
    end
    assert build(:project, log_file_order: [ "a" * 256 ]).valid?
  end

  test "a Log file order must be a list of names" do
    [ "service", { "service" => 1 }, [ 1 ], [ nil ] ].each do |order|
      project = build(:project, log_file_order: order)

      assert_not project.valid?, "#{order.inspect} should be refused"
      assert_equal [ "must be a list of Field names" ], project.errors[:log_file_order]
    end
  end

  test "to_param uses slug" do
    project = create(:project, slug: "my-project")

    assert_equal "my-project", project.to_param
  end

  test "platform returns ruby" do
    project = create(:project)

    assert_equal "ruby", project.platform
  end

  test "deleting a Project deletes what it owns, with the rows EventStore writes for it" do
    project = create(:project)
    issue = create(:issue, project: project)
    create(:issue_fingerprint, issue: issue)
    create(:comment, issue: issue)
    create(:project_key, project: project)
    stub_monitor_query_check(project)
    create(:monitor, project: project)
    chart = create(:dashboard_chart, project: project, dashboard: create(:dashboard, organization: project.organization))
    Project.connection.execute("INSERT INTO project_seen_cursors (project_id, event_uuid) VALUES (#{project.id}, 'abc')")
    other = create(:issue)

    project.destroy!

    assert_equal 0, Issue.where(project_id: project.id).count
    assert_equal 0, IssueFingerprint.where(project_id: project.id).count
    assert_equal 0, Comment.where(issue_id: issue.id).count
    assert_equal 0, ProjectKey.where(project_id: project.id).count
    assert_equal 0, LogMonitor.where(project_id: project.id).count
    assert_equal 0, ProjectIssueCounter.where(project_id: project.id).count
    assert_equal 0, Project.connection.select_value("SELECT COUNT(*) FROM project_seen_cursors WHERE project_id = #{project.id}")
    assert_not DashboardChart.exists?(chart.id)
    assert Dashboard.exists?(chart.dashboard_id)
    assert Issue.exists?(other.id)
  end
end
