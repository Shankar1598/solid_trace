# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Factory section in a Project's settings: the Project's repository,
# folder and default base branch, which Console reads and writes through
# the Dispatcher's API as the signed-in user. Admins edit; members read.
# It shows only when the Dispatcher URL is set.
class FactorySectionTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include TwoFactorTestHelper

  DISPATCHER = "http://10.100.0.3:8080"
  LIST_URL = "#{DISPATCHER}/api/v1/projects"

  setup do
    stub_request(:get, %r{/config\z}).to_return(status: 503)
    @organization = create(:organization)
    @admin = create(:user)
    @organization.organization_users.create!(user: @admin, role: :admin)
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    @project = create(:project, organization: @organization, name: "Shop")
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
  end

  test "an Admin saves the repository, folder and default base branch, and the Dispatcher gets them" do
    stub_list(settings("bitbucket.org/example/old", connection: "bitbucket-main"))
    stub_request(:put, settings_url).to_return(json(settings("bitbucket.org/example/shop", folder: "apps/shop",
                                                          base_branch: "develop", connection: "bitbucket-main")))
    sign_in_as(@admin)

    patch factory_url, params: { factory: { repository: " bitbucket.org/example/shop ", folder: "apps/shop",
                                            base_branch: "develop" } }

    assert_redirected_to project_url(@project.slug, org_slug: @organization.slug)
    assert_equal "Factory settings saved", flash[:notice]
    # The connection that the CLI set stays: Console sends it back as it read it.
    assert_requested(:put, settings_url) do |request|
      assert_match(/\ABearer stc_/, request.headers["Authorization"])
      assert_equal({ "repository" => "bitbucket.org/example/shop", "folder" => "apps/shop", "base_branch" => "develop",
                     "connection" => "bitbucket-main" }, JSON.parse(request.body))
    end
  end

  test "an Admin sees the values, an edit control and the link to the Runs page with the Project filter" do
    stub_list(settings("bitbucket.org/example/shop", folder: "apps/shop", base_branch: "develop"),
              settings("bitbucket.org/example/other", project: "other"))
    sign_in_as(@admin)

    page = project_page
    assert_equal true, page.dig("props", "can_manage_project")
    assert_equal "/#{@organization.slug}/runs?project=#{@project.slug}", page.dig("props", "factory_section", "runs_path")
    assert_equal "/#{@organization.slug}/projects/#{@project.slug}/factory", page.dig("props", "factory_section", "path")
    assert_equal({ "settings" => { "repository" => "bitbucket.org/example/shop", "folder" => "apps/shop",
                                   "base_branch" => "develop" } }, factory_prop)
  end

  test "a member sees the values and no edit control, and a write gets 403 with no call to the Dispatcher" do
    stub_list(settings("bitbucket.org/example/shop"))
    put = stub_request(:put, settings_url)
    sign_in_as(@member)

    assert_equal false, project_page.dig("props", "can_manage_project")
    assert_equal "bitbucket.org/example/shop", factory_prop.dig("settings", "repository")

    patch factory_url, params: { factory: { repository: "bitbucket.org/example/mine" } }

    assert_response :forbidden
    assert_not_requested put
  end

  test "a Project with no Factory settings shows empty values" do
    stub_list(settings("bitbucket.org/example/other", project: "other"))
    sign_in_as(@admin)

    assert_equal({ "settings" => nil }, factory_prop)
  end

  test "a first save sends no connection" do
    stub_list
    stub_request(:put, settings_url).to_return(json(settings("bitbucket.org/example/shop")))
    sign_in_as(@admin)

    patch factory_url, params: { factory: { repository: "bitbucket.org/example/shop" } }

    assert_requested(:put, settings_url) do |request|
      assert_equal({ "repository" => "bitbucket.org/example/shop", "folder" => "", "base_branch" => "",
                     "connection" => "" }, JSON.parse(request.body))
    end
  end

  test "the orphaned settings of a deleted Project with the same slug are not this Project's" do
    orphan = settings("bitbucket.org/example/old", folder: "apps/old", connection: "bitbucket-main")
      .merge(orphaned: true)
    stub_list(orphan)
    stub_request(:put, settings_url).to_return(json(settings("bitbucket.org/example/shop")))
    sign_in_as(@admin)

    assert_equal({ "settings" => nil }, factory_prop)

    patch factory_url, params: { factory: { repository: "bitbucket.org/example/shop" } }
    assert_requested(:put, settings_url) do |request|
      assert_equal "", JSON.parse(request.body)["connection"]
    end
  end

  test "this Project's settings show when an orphan with the same slug comes first in the list" do
    stub_list(settings("bitbucket.org/example/old").merge(orphaned: true), settings("bitbucket.org/example/shop"))
    sign_in_as(@admin)

    assert_equal "bitbucket.org/example/shop", factory_prop.dig("settings", "repository")
  end

  test "a value that the Dispatcher refuses comes back as a form error" do
    stub_list
    stub_request(:put, settings_url).to_return(json({ error: "the repository must be host/workspace/name" }, status: 400))
    sign_in_as(@admin)

    patch factory_url, params: { factory: { repository: "shop" } }, headers: inertia
    follow_redirect!(headers: inertia)

    assert_equal "The repository must be host/workspace/name", response.parsed_body.dig("props", "errors", "factory")
  end

  test "with no Dispatcher URL, the section is absent and Console calls no Dispatcher" do
    InstanceSetting.current.update!(dispatcher_url: nil)
    calls = stub_request(:any, /:8080/)
    sign_in_as(@admin)

    page = project_page
    assert_not page["props"].key?("factory")
    assert_nil page.dig("props", "factory_section")
    assert_nil page["deferredProps"]

    patch factory_url, params: { factory: { repository: "bitbucket.org/example/shop" } }
    assert_response :not_found
    assert_not_requested calls
  end

  test "the page shows at once, and loads the Factory settings after it shows" do
    list = stub_list
    sign_in_as(@admin)

    page = project_page
    assert_not page["props"].key?("factory")
    assert_equal [ "factory" ], page.dig("deferredProps", "default")
    assert_not_requested list
  end

  test "when the Dispatcher says its token is not active, the section shows a clear error" do
    stub_request(:get, LIST_URL).to_return(json({ error: "the Dispatcher's SolidTrace token is not active: the Admin " \
                                                         "who made it may have left the Organization" }, status: 503))
    sign_in_as(@member)

    assert_equal({ "error" => "The Dispatcher's SolidTrace token is not active: the Admin who made it may have left " \
                               "the Organization" }, factory_prop)
  end

  test "a Dispatcher that does not answer gives a clear error in the section, and a save shows it as an alert" do
    stub_request(:get, LIST_URL).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@admin)

    assert_match(/\AConsole could not connect to the Dispatcher at #{DISPATCHER}/, factory_prop["error"])

    patch factory_url, params: { factory: { repository: "bitbucket.org/example/shop" } }
    assert_match(/\AThe Dispatcher did not save the Factory settings: Console could not connect/, flash[:alert])
  end

  test "a person of another Organization gets 404" do
    outsider = create(:user)
    create(:organization).organization_users.create!(user: outsider, role: :admin)
    calls = stub_request(:any, /:8080/)
    sign_in_as(outsider)

    patch factory_url, params: { factory: { repository: "bitbucket.org/example/shop" } }

    assert_response :not_found
    assert_not_requested calls
  end

  private

  def factory_url
    "/#{@organization.slug}/projects/#{@project.slug}/factory"
  end

  def settings_url
    "#{DISPATCHER}/api/v1/projects/#{@project.slug}/settings"
  end

  def settings(repository, project: @project.slug, folder: "", base_branch: "", connection: "")
    { project: project, repository: repository, folder: folder, base_branch: base_branch, connection: connection,
      orphaned: false }
  end

  def stub_list(*projects)
    stub_request(:get, LIST_URL).to_return(json({ projects: projects }))
  end

  def json(body, status: 200)
    { status: status, headers: { "Content-Type" => "application/json" }, body: body.to_json }
  end

  def project_page
    get project_url(@project.slug, org_slug: @organization.slug), headers: inertia
    assert_response :success
    response.parsed_body
  end

  # The deferred factory prop, as the page loads it after it shows.
  def factory_prop
    get project_url(@project.slug, org_slug: @organization.slug),
      headers: inertia.merge("X-Inertia-Partial-Component" => "Projects/Show", "X-Inertia-Partial-Data" => "factory")
    assert_response :success
    response.parsed_body.dig("props", "factory")
  end
end
