# frozen_string_literal: true

require "test_helper"

# The Admin tools: Projects, their setup and keys, and members. Keys and
# invites are for the Organization's admins, as in the UI; any member makes
# a Project and reads its setup.
class McpAdminToolsTest < ActiveSupport::TestCase
  include McpTestHelper
  include ActionMailer::TestHelper

  setup do
    @acme = create(:organization, name: "Acme", slug: "acme")
    @admin = create(:user, name: "Ada", email: "ada@example.com")
    create(:organization_user, organization: @acme, user: @admin, role: :admin)
    @member = create(:user, name: "Bob", email: "bob@example.com")
    create(:organization_user, organization: @acme, user: @member, role: :member)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    @key = @shop.project_keys.create!
  end

  test "create_project makes a member's Project with its first key, linking to its page" do
    answer = call_tool(:create_project, user: @member, name: "Checkout", log_retention_days: 7)

    assert_not answer.error?, answer.text
    made = @acme.projects.find_by!(slug: "checkout")
    assert_equal({ "id" => made.id, "name" => "Checkout", "slug" => "checkout", "log_retention_days" => 7 }, answer.json["project"])
    key = made.project_keys.sole
    assert_equal key.public_key, answer.json.dig("key", "public_key")
    assert_equal "http://#{key.public_key}@console.test:4000/#{made.id}", answer.json.dig("key", "dsn")
    assert_equal "http://console.test/acme/projects/checkout", answer.link
  end

  test "create_project won't make a Project without a name" do
    answer = call_tool(:create_project, user: @admin, name: "  ")

    assert answer.error?
    assert_equal "Not saved: Name can't be blank.", answer.text
    assert_equal 1, @acme.projects.count
  end

  test "create_project won't make a second Project of a slug" do
    answer = call_tool(:create_project, user: @admin, name: "SHOP")

    assert answer.error?
    assert_equal "Not saved: Name gives the slug shop, which another of this Organization's Projects has.", answer.text
    assert_equal 1, @acme.projects.count
  end

  test "create_project won't make a Project whose name gives no slug" do
    [ "日本語", "Проект", "!!!" ].each do |name|
      answer = call_tool(:create_project, user: @admin, name: name)

      assert answer.error?, name
      assert_equal "Not saved: Name needs Latin letters or digits, since the Project's slug is made from them.", answer.text
    end
    assert_equal 1, @acme.projects.count
  end

  test "create_project won't make a Project whose slug is a page's" do
    answer = call_tool(:create_project, user: @admin, name: "New")

    assert answer.error?
    assert_equal "Not saved: Name gives the slug new, which is the New Project page's address.", answer.text
    assert_equal 1, @acme.projects.count
  end

  test "get_project_setup gives the DSN, Log ingest URL and key, and curl as the Project and Logs pages do" do
    second = @shop.project_keys.create!

    answer = call_tool(:get_project_setup, user: @member, project: "shop")

    assert_not answer.error?, answer.text
    setup = answer.json
    assert_equal "http://#{@key.public_key}@console.test:4000/#{@shop.id}", setup["dsn"]
    logs_url = "http://console.test:4000/api/logs"
    assert_equal({ "url" => logs_url, "key" => @key.public_key }, setup["logs"])
    assert_equal <<~CURL.chomp, setup["curl"]
      curl -X POST '#{logs_url}' \\
        -H 'Authorization: Bearer #{@key.public_key}' \\
        -H 'Content-Type: application/json' \\
        -d '{"message": "Hello from curl", "level": "info", "service": "api"}'
    CURL
    assert_equal [ @key.id, second.id ], setup["keys"].map { |key| key["id"] }
    assert_not setup["keys"].first.key?("secret_key")
    assert_equal "http://console.test/acme/projects/shop", answer.link
  end

  test "get_project_setup follows PUBLIC_INGEST_URL, and says when a Project has no key" do
    ENV["PUBLIC_INGEST_URL"] = "https://ingest.example.com/"
    @key.destroy!

    setup = call_tool(:get_project_setup, user: @member, project: "shop").json

    assert_nil setup["dsn"]
    assert_equal({ "url" => "https://ingest.example.com/api/logs", "key" => nil }, setup["logs"])
    assert_nil setup["curl"]
    assert_equal "This Project has no key. create_key makes one.", setup["note"]
  ensure
    ENV.delete("PUBLIC_INGEST_URL")
  end

  test "an admin's create_key gives the Project another key; a member's is refused" do
    refused = call_tool(:create_key, user: @member, project: "shop")
    assert refused.error?
    assert_equal "Only an Organization admin can make a Project's keys.", refused.text
    assert_equal 1, @shop.project_keys.count

    answer = call_tool(:create_key, user: @admin, project: "shop")
    assert_not answer.error?, answer.text
    made = @shop.project_keys.order(:id).last
    assert_equal made.id, answer.json.dig("key", "id")
    assert_equal "http://console.test/acme/projects/shop", answer.link
  end

  test "an admin's rotate_key gives the key new values; a member's is refused" do
    old = @key.public_key

    refused = call_tool(:rotate_key, user: @member, project: "shop", key: @key.id)
    assert refused.error?
    assert_equal "Only an Organization admin can rotate a Project's keys.", refused.text
    assert_equal old, @key.reload.public_key

    answer = call_tool(:rotate_key, user: @admin, project: "shop", key: @key.id)
    assert_not answer.error?, answer.text
    assert_not_equal old, @key.reload.public_key
    assert_equal @key.public_key, answer.json.dig("key", "public_key")
    assert_match "no longer send", answer.json["note"]
  end

  test "an admin's delete_key deletes it; a member's is refused" do
    refused = call_tool(:delete_key, user: @member, project: "shop", key: @key.id)
    assert_equal "Only an Organization admin can delete a Project's keys.", refused.text
    assert ProjectKey.exists?(@key.id)

    answer = call_tool(:delete_key, user: @admin, project: "shop", key: @key.id)
    assert_equal "Deleted key #{@key.id} of shop.", answer.body
    assert_not ProjectKey.exists?(@key.id)
  end

  test "another Project's key is not found" do
    theirs = create(:project, organization: @acme, slug: "web").project_keys.create!

    answer = call_tool(:rotate_key, user: @admin, project: "shop", key: theirs.id)

    assert answer.error?
    assert_equal "No such key. get_project_setup lists them.", answer.text
  end

  test "rotate_key and delete_key are marked destructive" do
    [ McpTools::RotateKey, McpTools::DeleteKey ].each do |tool|
      assert tool.annotations_value.destructive_hint, tool.name_value
    end
  end

  test "list_members lists the Organization's members by name with their roles, linking to its Members page" do
    create(:organization_user, organization: @acme, user: create(:user, name: "Zed")).discard!

    answer = call_tool(:list_members, user: @member)

    assert_equal [ { "name" => "Ada", "email" => "ada@example.com", "role" => "admin" },
                   { "name" => "Bob", "email" => "bob@example.com", "role" => "member" } ], answer.json["members"]
    assert_equal "http://console.test/acme/settings/organization_users", answer.link
  end

  test "an admin's invite_member adds someone new and emails them" do
    answer = nil
    assert_enqueued_emails 1 do
      answer = call_tool(:invite_member, user: @admin, email: "grace@example.com", name: "Grace")
    end

    assert_not answer.error?, answer.text
    assert_equal({ "organization" => "acme", "member" => { "name" => "Grace", "email" => "grace@example.com", "role" => "member" },
                   "outcome" => "added", "emailed" => true }, answer.json)
    assert_equal "http://console.test/acme/settings/organization_users", answer.link
  end

  test "invite_member says why someone wasn't invited" do
    answer = call_tool(:invite_member, user: @admin, email: "bob@example.com")

    assert answer.error?
    assert_equal "Not invited: User is already a member of this organization.", answer.text
  end

  test "a member's invite_member is refused" do
    answer = nil
    assert_no_difference "User.count" do
      answer = call_tool(:invite_member, user: @member, email: "grace@example.com")
    end

    assert answer.error?
    assert_equal "Only an Organization admin can invite members.", answer.text
  end
end
