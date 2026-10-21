# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Log transform tools: reading a Project's program, its versions and
# running counts; previewing a draft; and saving one, for admins. Each answer
# links to the Project's Log transform page. EventStore is stubbed.
class McpLogTransformToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  ERROR = {
    "message" => "call to undefined function", "code" => 105, "start" => 0, "end" => 12,
    "text" => "error[E105]: call to undefined function\n  ┌─ :1:1\n  │\n1 │ http_request(\"x\")\n  │ ^^^^^^^^^^^^ undefined function",
  }.freeze
  PAGE = "#{McpTestHelper::CONSOLE_URL}/acme/projects/shop/log_transform".freeze

  setup do
    @acme = create(:organization, slug: "acme")
    @admin = create(:user, name: "Ada", email: "ada@example.com")
    create(:organization_user, organization: @acme, user: @admin, role: :admin)
    @member = create(:user, name: "Bob", email: "bob@example.com")
    create(:organization_user, organization: @acme, user: @member, role: :member)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    @check_url = "#{EventStore.base_url}/api/#{@shop.id}/logs/transform/check"
  end

  # get_log_transform

  test "get_log_transform gives the current program, who saved it and when, the last 10 versions and the running counts" do
    gone = create(:user, name: "Gone")
    versions = (1..12).map do |n|
      travel_to(Time.utc(2026, 10, 21, 9, n)) { @shop.log_transform_versions.create!(program: ".n = #{n}", user: n == 11 ? gone : @admin) }
    end
    gone.destroy!
    stats = { "version_id" => versions.last.id, "processed" => 120, "dropped" => 3, "failed" => 2, "average_micros" => 8.5 }
    stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/logs/transform/stats").to_return(status: 200, body: stats.to_json)

    answer = call_tool(:get_log_transform, user: @member, project: "shop")

    assert_not answer.error?
    ada = { "name" => "Ada", "email" => "ada@example.com" }
    assert_equal({
      "project" => "shop", "program" => ".n = 12", "version" => versions.last.id, "saved_by" => ada,
      "saved_at" => "2026-10-21T09:12:00Z", "current" => true, "running_counts" => stats,
    }, answer.json.except("versions"))
    assert_equal versions.last(10).reverse.map(&:id), answer.json["versions"].pluck("version")
    assert_equal({ "version" => versions[10].id, "saved_by" => nil, "saved_at" => "2026-10-21T09:11:00Z", "bytes" => 7 },
      answer.json["versions"].second)
    assert_equal PAGE, answer.link
  end

  test "get_log_transform gives an older version's program, and not another Project's" do
    old = @shop.log_transform_versions.create!(program: ".a = 1", user: @admin)
    @shop.log_transform_versions.create!(program: ".a = 2", user: @admin)
    theirs = create(:project, organization: @acme, slug: "theirs").log_transform_versions.create!(program: ".b = 1")
    stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/logs/transform/stats")
      .to_return(status: 200, body: { version_id: 0, processed: 0, dropped: 0, failed: 0, average_micros: 0 }.to_json)

    answer = call_tool(:get_log_transform, user: @member, project: "shop", version: old.id)

    assert_equal [ ".a = 1", old.id, false ], answer.json.values_at("program", "version", "current")

    missing = call_tool(:get_log_transform, user: @member, project: "shop", version: theirs.id)
    assert missing.error?
    assert_equal "No such Log transform version. get_log_transform lists them.", missing.text
  end

  test "get_log_transform says when a Project has no Log transform, and passes on a compile error" do
    stats = { "version_id" => 0, "processed" => 0, "dropped" => 0, "failed" => 0, "average_micros" => 0,
              "compile_error" => "error[E105]: call to undefined function" }
    stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/logs/transform/stats").to_return(status: 200, body: stats.to_json)

    answer = call_tool(:get_log_transform, user: @member, project: "shop")

    assert_equal({
      "project" => "shop", "program" => "", "note" => "The Project has no Log transform: its Logs are stored as they arrive.",
      "versions" => [], "running_counts" => stats,
    }, answer.json)
  end

  # update_log_transform

  test "an admin's update_log_transform saves a program that compiles, saying stored Logs don't change" do
    travel_to Time.utc(2026, 10, 21, 9) do
      stub_request(:post, @check_url).with(body: { program: ".a = 1" }.to_json)
        .to_return(status: 200, body: { errors: [], warnings: [] }.to_json)

      answer = call_tool(:update_log_transform, user: @admin, project: "shop", program: ".a = 1")

      assert_not answer.error?
      version = @shop.log_transform
      assert_equal ".a = 1", version.program
      assert_equal({
        "project" => "shop", "outcome" => "saved", "version" => version.id,
        "saved_by" => { "name" => "Ada", "email" => "ada@example.com" }, "saved_at" => "2026-10-21T09:00:00Z",
        "note" => "Log processing runs it on the Logs it processes from now on, within seconds. Logs already stored don't change.",
      }, answer.json)
      assert_equal PAGE, answer.link
    end
  end

  test "update_log_transform refuses a program that doesn't compile with VRL's errors, saving nothing" do
    stub_request(:post, @check_url).to_return(status: 200, body: { errors: [ ERROR, ERROR.merge("text" => "error[E100]: second") ] }.to_json)

    answer = call_tool(:update_log_transform, user: @admin, project: "shop", program: "http_request(\"x\")")

    assert answer.error?
    assert_equal "Not saved: the program doesn't compile.\n#{ERROR['text']}\nerror[E100]: second", answer.text
    assert_nil @shop.log_transform
  end

  test "update_log_transform with the current program changes nothing" do
    current = @shop.log_transform_versions.create!(program: ".a = 1", user: @member)

    answer = call_tool(:update_log_transform, user: @admin, project: "shop", program: ".a = 1")

    assert_equal [ "unchanged", current.id, { "name" => "Bob", "email" => "bob@example.com" } ],
      answer.json.values_at("outcome", "version", "saved_by")
    assert_equal 1, @shop.log_transform_versions.count
  end

  test "a member's update_log_transform is refused: only an admin can change it" do
    answer = call_tool(:update_log_transform, user: @member, project: "shop", program: ".a = 1")

    assert answer.error?
    assert_equal "Only an Organization admin can change a Project's Log transform.", answer.text
    assert_nil @shop.log_transform
  end

  test "update_log_transform is marked destructive, and its description explains the program" do
    tool = McpTools::UpdateLogTransform

    assert tool.annotations_value.destructive_hint
    [ "abort", "solidtrace.transform_error", "http_request", "Logs already stored don't change",
      "https://vector.dev/docs/reference/vrl/functions/", ".http.method" ].each do |part|
      assert_includes tool.description_value, part
    end
  end
end
