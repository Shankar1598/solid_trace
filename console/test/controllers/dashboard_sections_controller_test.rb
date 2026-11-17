# frozen_string_literal: true

require "test_helper"

class DashboardSectionsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @project = create(:project, organization: @organization)
    @dashboard = create(:dashboard, organization: @organization, name: "Checkout")
    @section = create(:dashboard_section, dashboard: @dashboard, title: "Errors", position: 0)
    @chart = create(:dashboard_chart, dashboard: @dashboard, project: @project, section: @section, title: "Errors", y: 0, h: 4)

    @outsider = create(:user)
    create(:organization_user, organization: create(:organization), user: @outsider, role: :admin)

    sign_in_as(@member)
  end

  test "a member opens a Dashboard with its Sections in order, and each chart's Section" do
    second = create(:dashboard_section, dashboard: @dashboard, title: "Latency", position: 1)

    get dashboard_url(@dashboard, org_slug: @organization.slug), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }

    dashboard = response.parsed_body.dig("props", "dashboard")
    assert_equal [ [ @section.id, "Errors", 0 ], [ second.id, "Latency", 1 ] ], dashboard["sections"].map { |s| s.values_at("id", "title", "position") }
    assert_equal @section.id, dashboard["charts"].sole["section_id"]
  end

  test "a member adds a Section, last unless a position is given, keeping what the page showed" do
    post dashboard_sections_url(@dashboard, org_slug: @organization.slug, range: "24h"), params: { section: { title: "Latency" } }

    assert_redirected_to dashboard_url(@dashboard, org_slug: @organization.slug, range: "24h")
    assert_equal %w[Errors Latency], @dashboard.sections.reload.map(&:title)

    post dashboard_sections_url(@dashboard, org_slug: @organization.slug), params: { section: { title: "Top", position: 0 } }, as: :json
    assert_response :created
    assert_equal [ "Top", 0 ], response.parsed_body.values_at("title", "position")
    assert_equal %w[Top Errors Latency], @dashboard.sections.reload.map(&:title)
  end

  test "a Section without a title is refused, naming the field" do
    post dashboard_sections_url(@dashboard, org_slug: @organization.slug), params: { section: { title: " " } }, as: :json

    assert_response :unprocessable_content
    assert_equal [ "can't be blank" ], response.parsed_body.dig("errors", "title")
    assert_equal 1, @dashboard.sections.count
  end

  test "a member renames a Section and moves it down and up" do
    below = create(:dashboard_section, dashboard: @dashboard, title: "Latency", position: 1)

    patch dashboard_section_url(@dashboard, @section, org_slug: @organization.slug), params: { section: { title: "Failures" } }
    assert_redirected_to dashboard_url(@dashboard, org_slug: @organization.slug)
    assert_equal "Failures", @section.reload.title

    patch dashboard_section_url(@dashboard, @section, org_slug: @organization.slug), params: { section: { position: 1 } }, as: :json
    assert_response :success
    assert_equal [ below, @section ], @dashboard.sections.reload

    patch dashboard_section_url(@dashboard, @section, org_slug: @organization.slug), params: { section: { position: 0 } }, as: :json
    assert_equal [ @section, below ], @dashboard.sections.reload

    patch dashboard_section_url(@dashboard, @section, org_slug: @organization.slug), params: { section: { position: -1 } }, as: :json
    assert_response :unprocessable_content
    assert_equal [ "must be 0 or more" ], response.parsed_body.dig("errors", "position")
  end

  test "a member deletes a Section, and its charts stay in no Section" do
    delete dashboard_section_url(@dashboard, @section, org_slug: @organization.slug)

    assert_redirected_to dashboard_url(@dashboard, org_slug: @organization.slug)
    assert_not DashboardSection.exists?(@section.id)
    assert_nil @chart.reload.section_id
  end

  test "a member adds a chart in a Section, under its charts, and moves it to no Section" do
    post dashboard_charts_url(@dashboard, org_slug: @organization.slug), params: { chart: {
      project_id: @project.id, title: "More errors", query: "summarize count()", viz: "line", section_id: @section.id,
    } }, as: :json

    assert_response :created
    added = @dashboard.charts.find(response.parsed_body["id"])
    assert_equal [ @section.id, 4 ], [ added.section_id, added.y ]

    post duplicate_dashboard_chart_url(@dashboard, added, org_slug: @organization.slug), as: :json
    assert_equal [ @section.id, 8 ], response.parsed_body.values_at("section_id", "y")

    patch dashboard_chart_url(@dashboard, added, org_slug: @organization.slug), params: { chart: { section_id: "" } }, as: :json
    assert_response :success
    assert_equal [ nil, 0 ], added.reload.values_at(:section_id, :y)
  end

  test "a chart in another Dashboard's Section is refused" do
    theirs = create(:dashboard_section, dashboard: create(:dashboard, organization: @organization))

    patch dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), params: { chart: { section_id: theirs.id } }, as: :json

    assert_response :unprocessable_content
    assert response.parsed_body.dig("errors", "section").present?
    assert_equal @section.id, @chart.reload.section_id
  end

  test "someone outside the Organization finds none of its Sections" do
    sign_in_as(@outsider)
    org = @organization.slug

    post dashboard_sections_url(@dashboard, org_slug: org), params: { section: { title: "Mine" } }, as: :json
    assert_response :not_found
    patch dashboard_section_url(@dashboard, @section, org_slug: org), params: { section: { title: "Mine" } }, as: :json
    assert_response :not_found
    delete dashboard_section_url(@dashboard, @section, org_slug: org), as: :json
    assert_response :not_found

    assert_equal [ "Errors" ], @dashboard.sections.reload.map(&:title)
  end

  test "a Section of another Dashboard isn't found under this one" do
    theirs = create(:dashboard_section, dashboard: create(:dashboard, organization: @organization), title: "Theirs")

    patch dashboard_section_url(@dashboard, theirs, org_slug: @organization.slug), params: { section: { title: "Mine" } }, as: :json

    assert_response :not_found
    assert_equal "Theirs", theirs.reload.title
  end
end
