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
