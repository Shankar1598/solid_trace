# frozen_string_literal: true

require "test_helper"

class DashboardSectionTest < ActiveSupport::TestCase
  setup do
    @dashboard = create(:dashboard)
    @project = create(:project, organization: @dashboard.organization)
  end

  test "a Section's title is present and at most 200 characters" do
    assert build(:dashboard_section, dashboard: @dashboard, title: "a" * 200).valid?
    assert build(:dashboard_section, dashboard: @dashboard, title: " ").invalid?
    assert build(:dashboard_section, dashboard: @dashboard, title: "a" * 201).invalid?
  end

  test "a Dashboard's Sections are in position order" do
    late = create(:dashboard_section, dashboard: @dashboard, title: "Late", position: 1)
    early = create(:dashboard_section, dashboard: @dashboard, title: "Early", position: 0)

    assert_equal [ early, late ], @dashboard.sections.reload
  end

  test "save_at! puts a new Section last, or at the position given, and moves the others to make room" do
    first = @dashboard.sections.new(title: "First").save_at!
    second = @dashboard.sections.new(title: "Second").save_at!
    top = @dashboard.sections.new(title: "Top").save_at!(0)
    far = @dashboard.sections.new(title: "Far").save_at!(99)

    assert_equal [ [ "Top", 0 ], [ "First", 1 ], [ "Second", 2 ], [ "Far", 3 ] ], @dashboard.sections.reload.map { |s| [ s.title, s.position ] }
    assert_equal [ 0, 1, 2, 3 ], [ top, first, second, far ].map { |s| s.reload.position }
  end

  test "save_at! moves a saved Section to the position given, and keeps its place with none" do
    a, b, c = %w[A B C].map { |title| @dashboard.sections.new(title: title).save_at! }

    c.save_at!(0)
    assert_equal %w[C A B], @dashboard.sections.reload.map(&:title)
    c.save_at!("2")
    assert_equal %w[A B C], @dashboard.sections.reload.map(&:title)

    b.title = "Bee"
    b.save_at!
    assert_equal [ %w[A Bee C], [ 0, 1, 2 ] ], [ @dashboard.sections.reload.map(&:title), @dashboard.sections.map(&:position) ]
    assert_equal 0, a.reload.position
  end

  test "save_at! refuses a position that isn't a whole number of 0 or more, naming it" do
    section = @dashboard.sections.new(title: "Errors")

    error = assert_raises(ActiveRecord::RecordInvalid) { section.save_at!(-1) }
    assert_equal "Position must be 0 or more", error.record.errors.full_messages.sole
    assert_raises(ActiveRecord::RecordInvalid) { @dashboard.sections.new(title: "Errors").save_at!("top") }
    assert_equal 0, @dashboard.sections.count
  end

  test "a Dashboard has at most MAX_SECTIONS Sections" do
    Dashboard::MAX_SECTIONS.times { |i| create(:dashboard_section, dashboard: @dashboard, position: i) }

    extra = build(:dashboard_section, dashboard: @dashboard)
    assert_not extra.valid?
    assert_equal [ "has at most #{Dashboard::MAX_SECTIONS} Sections" ], extra.errors[:dashboard]
  end

  test "deleting a Section keeps its Charts in no Section, under the Charts there, in the same places to each other" do
    section = create(:dashboard_section, dashboard: @dashboard)
    create(:dashboard_chart, dashboard: @dashboard, project: @project, y: 0, h: 3)
    left = create(:dashboard_chart, dashboard: @dashboard, project: @project, section: section, x: 0, y: 0, h: 4)
    under = create(:dashboard_chart, dashboard: @dashboard, project: @project, section: section, x: 6, y: 4, h: 2)

    section.destroy!

    assert_equal [ nil, 0, 3 ], left.reload.values_at(:section_id, :x, :y)
    assert_equal [ nil, 6, 7 ], under.reload.values_at(:section_id, :x, :y)
    assert_equal 3, @dashboard.charts.count
  end

  test "deleting a Dashboard deletes its Sections and Charts" do
    section = create(:dashboard_section, dashboard: @dashboard)
    chart = create(:dashboard_chart, dashboard: @dashboard, project: @project, section: section)

    @dashboard.destroy!

    assert_not DashboardSection.exists?(section.id)
    assert_not DashboardChart.exists?(chart.id)
  end

  test "a Chart's Section must be one of its Dashboard's" do
    theirs = create(:dashboard_section, dashboard: create(:dashboard, organization: @dashboard.organization))
    ours = create(:dashboard_section, dashboard: @dashboard)

    assert build(:dashboard_chart, dashboard: @dashboard, project: @project, section: ours).valid?
    assert build(:dashboard_chart, dashboard: @dashboard, project: @project, section: nil).valid?
    [ theirs.id, DashboardSection.maximum(:id) + 1 ].each do |section_id|
      chart = build(:dashboard_chart, dashboard: @dashboard, project: @project, section_id: section_id)
      assert_not chart.valid?
      assert_equal [ "must be a Section of the Chart's Dashboard" ], chart.errors[:section]
    end
  end

  test "bottom is the row under every chart of a Section, or of no Section" do
    section = create(:dashboard_section, dashboard: @dashboard)
    create(:dashboard_chart, dashboard: @dashboard, project: @project, y: 0, h: 5)
    create(:dashboard_chart, dashboard: @dashboard, project: @project, section: section, y: 2, h: 2)

    assert_equal [ 5, 4, 0 ], [ @dashboard.bottom, @dashboard.bottom(section.id), @dashboard.bottom(section.id + 1) ]
  end

  test "place puts a new chart beside the last row when there is space, else under every chart of its group" do
    section = create(:dashboard_section, dashboard: @dashboard)
    assert_equal [ 0, 0 ], @dashboard.place(section.id, 6)

    create(:dashboard_chart, dashboard: @dashboard, project: @project, section: section, x: 0, y: 0, w: 6, h: 4)
    assert_equal [ 6, 0 ], @dashboard.place(section.id, 6)
    assert_equal [ 0, 4 ], @dashboard.place(section.id, 7)

    create(:dashboard_chart, dashboard: @dashboard, project: @project, section: section, x: 6, y: 0, w: 6, h: 4)
    assert_equal [ 0, 4 ], @dashboard.place(section.id, 6)
    assert_equal [ 0, 0 ], @dashboard.place(nil, 6)
  end
end
