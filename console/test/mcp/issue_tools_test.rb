# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Issue tools: an Organization's Issues listed as its Issues page lists
# them, an Issue with one of its Events, resolving, assigning and commenting,
# each answer linking to the Issue.
class McpIssueToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @user = create(:user, name: "Ann", email: "ann@acme.test")
    @acme = create(:organization, slug: "acme")
    @membership = create(:organization_user, organization: @acme, user: @user, role: :member)
    @shop = create(:project, organization: @acme, slug: "shop")
    @api = create(:project, organization: @acme, slug: "api")
    @issue = create(:issue, project: @shop, title: "NoMethodError in checkout", culprit: "CheckoutController#create",
      last_seen_at: Time.utc(2026, 10, 3, 9))

    theirs = create(:organization, slug: "theirs")
    @their_issue = create(:issue, project: create(:project, organization: theirs, slug: "secret"), title: "Theirs")
  end

  # Listing

  test "list_issues answers the Organization's Issues, the most recently seen first, linking to its Issues page over all time" do
    create(:issue, project: @api, title: "Timeout in api", last_seen_at: Time.utc(2026, 10, 3, 10), status: :resolved)
    @issue.update!(assignee: @membership)

    answer = call_tool(:list_issues, user: @user)

    assert_not answer.error?
    assert_equal [ "acme", 1, 1, 2 ], answer.json.values_at("organization", "page", "total_pages", "total_count")
    assert_equal [ [ "api", "Timeout in api", "resolved" ], [ "shop", "NoMethodError in checkout", "unresolved" ] ],
      answer.json["issues"].map { |issue| issue.values_at("project", "title", "status") }
    assert_equal({ "project" => "shop", "number" => @issue.number, "title" => "NoMethodError in checkout",
                   "culprit" => "CheckoutController#create", "status" => "unresolved", "kind" => "error", "events_count" => 0,
                   "first_seen_at" => @issue.created_at.iso8601, "last_seen_at" => "2026-10-03T09:00:00Z",
                   "assignee" => { "name" => "Ann", "email" => "ann@acme.test" } },
      answer.json["issues"].last)
    assert_equal "http://console.test/acme/issues?range=all", answer.link
  end

  test "list_issues filters by Project, status and title, sorts by first seen, and links to the Issues page so filtered" do
    create(:issue, project: @shop, title: "NoMethodError in cart", status: :resolved)
    newer = create(:issue, project: @shop, title: "NoMethodError in search", first_seen_at: 1.minute.from_now)
    create(:issue, project: @api, title: "NoMethodError in api")

    answer = call_tool(:list_issues, user: @user, project: "shop", status: "unresolved", query: "NoMethod", sort: "first_seen")

    assert_not answer.error?
    assert_equal [ newer.number, @issue.number ], answer.json["issues"].map { |issue| issue["number"] }
    assert_equal "http://console.test/acme/issues?project_id=#{@shop.id}&query=NoMethod&range=all&sort=first_seen&status=unresolved", answer.link
  end

  test "list_issues gives the page asked for, #{IssueList::PER_PAGE} a page" do
    IssueList::PER_PAGE.times { |i| create(:issue, project: @api, title: "Old #{i}", last_seen_at: Time.utc(2026, 10, 1)) }

    answer = call_tool(:list_issues, user: @user, page: 2)

    assert_equal [ 2, 2, IssueList::PER_PAGE + 1 ], answer.json.values_at("page", "total_pages", "total_count")
    assert_equal 1, answer.json["issues"].size
    assert_equal "http://console.test/acme/issues?page=2&range=all", answer.link
  end

  test "list_issues refuses another Organization's Project" do
    answer = call_tool(:list_issues, user: @user, project: "secret")

    assert answer.error?
    assert_equal "No Project secret in acme. list_projects lists them.", answer.text
  end

  # Reading one

  test "get_issue answers the Issue with its latest Event, cut short, and the Events either side, linking to the Issue" do
    stub_event_context(prev_uuid: "evt-0")
    stub_payload(
      level: "error", release: "shop@1.2", environment: "production",
      exception: { values: [
        { type: "KeyError", value: "key not found: :sku", stacktrace: { frames: [ frame("lib/cart.rb", 3, in_app: true) ] } },
        { type: "NoMethodError", value: "undefined method 'price' for nil", stacktrace: { frames: [
          frame("gems/rack.rb", 10, in_app: false), frame("app/models/cart.rb", 20, in_app: true),
          frame("app/controllers/checkout_controller.rb", 30, in_app: true), frame("gems/active_support.rb", 40, in_app: false)
        ] } }
      ] },
      tags: [ [ "browser", "Firefox" ], [ "url", "/checkout" ] ],
      contexts: { trace: { trace_id: "4bf92f3577b34da6a3ce929d0e0e4736", span_id: "00f067aa0ba902b7" } },
    )

    answer = call_tool(:get_issue, user: @user, project: "shop", issue: @issue.number)

    assert_not answer.error?
    assert_equal [ "shop", @issue.number, "NoMethodError in checkout", "unresolved" ], answer.json.values_at("project", "number", "title", "status")
    assert_equal({
      "id" => "evt-1", "time" => "2026-10-03T09:00:00.000Z", "environment" => "production", "level" => "error", "release" => "shop@1.2",
      "exceptions" => [
        { "type" => "NoMethodError", "value" => "undefined method 'price' for nil", "frames" => [
          { "filename" => "app/controllers/checkout_controller.rb", "lineno" => 30, "function" => "create", "context_line" => "line 30" },
          { "filename" => "app/models/cart.rb", "lineno" => 20, "function" => "create", "context_line" => "line 20" }
        ] },
        { "type" => "KeyError", "value" => "key not found: :sku", "frames" => [
          { "filename" => "lib/cart.rb", "lineno" => 3, "function" => "create", "context_line" => "line 3" }
        ] }
      ],
      "tags" => { "browser" => "Firefox", "url" => "/checkout" },
      "trace_id" => "4bf92f3577b34da6a3ce929d0e0e4736",
    }, answer.json["event"])
    assert_equal [ "evt-0", nil ], answer.json.values_at("older_event", "newer_event")
    assert_equal({ "tool" => "query_logs", "arguments" => {
      "organization" => "acme", "project" => "shop", "query" => 'where trace_id == "4bf92f3577b34da6a3ce929d0e0e4736"',
      "from" => "2026-10-03T08:00:00.000Z", "to" => "2026-10-03T10:00:00.000Z",
    } }, answer.json["trace_logs"])
    assert_equal "http://console.test/acme/projects/shop/issues/#{@issue.number}", answer.link
  end

  test "get_issue answers the Event asked for, without a trace hint when it has no trace id, linking to it" do
    context = stub_event_context(uuid: "evt-1", next_uuid: "evt-2")
    stub_payload(message: { formatted: "Payment declined" }, tags: { "region" => "eu" })

    answer = call_tool(:get_issue, user: @user, project: "shop", issue: @issue.number, event: "evt-1")

    assert_not answer.error?
    assert_requested context
    assert_equal({ "id" => "evt-1", "time" => "2026-10-03T09:00:00.000Z", "environment" => "production",
                   "message" => "Payment declined", "tags" => { "region" => "eu" } }, answer.json["event"])
    assert_equal [ nil, "evt-2", nil ], answer.json.values_at("older_event", "newer_event", "trace_logs")
    assert_equal "http://console.test/acme/projects/shop/issues/#{@issue.number}?event_id=evt-1", answer.link
  end

  test "get_issue answers the Issue while EventStore can't read Events, saying why its Event is left out" do
    create(:issue_fingerprint, issue: @issue)
    why = "Events can't be read now: ClickHouse doesn't answer: dial tcp 127.0.0.1:4200: connect: connection refused"
    stub_request(:get, %r{/api/#{@shop.id}/events}).to_return(status: 503, body: { error: why }.to_json)

    answer = call_tool(:get_issue, user: @user, project: "shop", issue: @issue.number)

    assert_not answer.error?
    assert_equal [ "NoMethodError in checkout", nil, why ], answer.json.values_at("title", "event", "events_unavailable")
    assert_equal "http://console.test/acme/projects/shop/issues/#{@issue.number}", answer.link
  end

  test "get_issue says when EventStore can't be reached, and when it didn't give the Event's payload" do
    create(:issue_fingerprint, issue: @issue)
    stub_request(:get, %r{/api/#{@shop.id}/events}).to_raise(Errno::ECONNREFUSED)

    answer = call_tool(:get_issue, user: @user, project: "shop", issue: @issue.number)
    assert_equal "Events can't be read now: EventStore can't be reached.", answer.json["events_unavailable"]

    WebMock.reset!
    stub_event_context
    stub_request(:get, %r{/api/#{@shop.id}/events/evt-1\z}).to_return(status: 500)

    answer = call_tool(:get_issue, user: @user, project: "shop", issue: @issue.number)
    assert_equal [ "evt-1", { "indexed" => "yes" }, nil ], answer.json["event"].values_at("id", "tags", "trace_id")
    assert_match "EventStore didn't give this Event's payload", answer.json["event"]["missing"]
  end

  test "get_issue answers an Issue with no Event yet, and refuses an Event it doesn't have" do
    assert_nil call_tool(:get_issue, user: @user, project: "shop", issue: @issue.number).json["event"]

    create(:issue_fingerprint, issue: @issue)
    stub_request(:get, %r{/api/#{@shop.id}/events/context}).to_return(status: 404, body: { error: "Event not found" }.to_json)
    answer = call_tool(:get_issue, user: @user, project: "shop", issue: @issue.number, event: "nope")

    assert answer.error?
    assert_equal "No Event nope in this Issue. get_issue without event gives its latest.", answer.text
  end

  test "get_issue gives the Issue's last comments, oldest first" do
    other = create(:organization_user, organization: @acme, user: create(:user, name: "Bo", email: "bo@acme.test"))
    @issue.comments.create!(organization_user: @membership, content: "<p>Looking</p>", created_at: 2.minutes.ago)
    @issue.comments.create!(organization_user: other, content: "<p>Fixed in <b>#42</b></p>", created_at: 1.minute.ago)

    comments = call_tool(:get_issue, user: @user, project: "shop", issue: @issue.number).json["comments"]

    assert_equal [ [ "ann@acme.test", "Looking" ], [ "bo@acme.test", "Fixed in #42" ] ],
      comments.map { |comment| [ comment["user"]["email"], comment["body"] ] }
  end

  test "get_issue finds Issues only in the user's Organizations" do
    assert_equal "No such Issue. list_issues lists them.", call_tool(:get_issue, user: @user, project: "shop", issue: 999).text
    assert_equal "No Project secret in acme. list_projects lists them.",
      call_tool(:get_issue, user: @user, project: "secret", issue: @their_issue.number).text
  end

  # Changing

  test "update_issue resolves and unresolves an Issue, linking to it" do
    answer = call_tool(:update_issue, user: @user, project: "shop", issue: @issue.number, status: "resolved")

    assert_not answer.error?
    assert_equal "resolved", @issue.reload.status
    assert_equal [ @issue.number, "resolved" ], answer.json.values_at("number", "status")
    assert_equal "http://console.test/acme/projects/shop/issues/#{@issue.number}", answer.link

    call_tool(:update_issue, user: @user, project: "shop", issue: @issue.number, status: "unresolved")
    assert_equal "unresolved", @issue.reload.status
  end

  test "update_issue assigns an Issue to a member by email, and unassigns it" do
    bo = create(:organization_user, organization: @acme, user: create(:user, name: "Bo", email: "bo@acme.test"))

    answer = call_tool(:update_issue, user: @user, project: "shop", issue: @issue.number, assignee: " Bo@Acme.test ")

    assert_not answer.error?
    assert_equal bo, @issue.reload.assignee
    assert_equal({ "name" => "Bo", "email" => "bo@acme.test" }, answer.json["assignee"])

    answer = call_tool(:update_issue, user: @user, project: "shop", issue: @issue.number, assignee: "unassigned", status: "resolved")

    assert_nil @issue.reload.assignee
    assert_equal [ nil, "resolved" ], answer.json.values_at("assignee", "status")
  end

  test "update_issue refuses an email that isn't a member's, and a call that changes nothing" do
    outsider = create(:user, email: "eve@theirs.test")
    create(:organization_user, organization: @their_issue.project.organization, user: outsider)
    removed = create(:organization_user, organization: @acme, user: create(:user, email: "gone@acme.test"))
    removed.discard!

    [ "eve@theirs.test", "gone@acme.test", "nobody@acme.test" ].each do |email|
      answer = call_tool(:update_issue, user: @user, project: "shop", issue: @issue.number, assignee: email)

      assert answer.error?
      assert_equal "No member #{email} in acme. list_members lists them.", answer.text
    end
    assert_nil @issue.reload.assignee

    answer = call_tool(:update_issue, user: @user, project: "shop", issue: @issue.number)
    assert_equal "Give status, assignee or both to change.", answer.text
    assert_equal "No such Issue. list_issues lists them.", call_tool(:update_issue, user: @user, project: "shop", issue: 999, status: "resolved").text
  end

  test "add_comment adds the user's comment, as escaped paragraphs, linking to the Issue" do
    answer = call_tool(:add_comment, user: @user, project: "shop", issue: @issue.number, body: "Fixed by <b>#42</b>.\n\nDeploying now.")

    assert_not answer.error?
    comment = @issue.comments.sole
    assert_equal @membership, comment.organization_user
    assert_equal "<p>Fixed by &lt;b&gt;#42&lt;/b&gt;.</p>\n\n<p>Deploying now.</p>", comment.content.body.to_html
    assert_equal [ "shop", @issue.number ], answer.json.values_at("project", "issue")
    assert_equal [ comment.id, { "name" => "Ann", "email" => "ann@acme.test" }, "Fixed by <b>#42</b>.\n\nDeploying now." ],
      answer.json["comment"].values_at("id", "user", "body")
    assert_equal "http://console.test/acme/projects/shop/issues/#{@issue.number}", answer.link
  end

  test "add_comment refuses a blank body" do
    answer = call_tool(:add_comment, user: @user, project: "shop", issue: @issue.number, body: "  ")

    assert answer.error?
    assert_equal "Give the comment's body.", answer.text
    assert_empty @issue.comments
  end

  private

  # Answers the Issue's Event context: its Event evt-1 with the Events
  # either side, once the Issue has a fingerprint to ask for.
  def stub_event_context(uuid: nil, prev_uuid: nil, next_uuid: nil)
    fingerprint = @issue.issue_fingerprints.first || create(:issue_fingerprint, issue: @issue)
    query = { fingerprint_ids: fingerprint.id.to_s, uuid: uuid }.compact
    event = { uuid: "evt-1", project_id: @shop.id, issue_fingerprint_id: fingerprint.id, environment: "production",
              timestamp: "2026-10-03T09:00:00Z", tags: { "indexed" => "yes" } }
    stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/events/context").with(query: query)
      .to_return(body: { event: event, prev_uuid: prev_uuid.to_s, next_uuid: next_uuid.to_s }.to_json)
  end

  def stub_payload(payload)
    stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/events/evt-1").to_return(body: payload.to_json)
  end

  def frame(filename, lineno, in_app:)
    { filename: filename, lineno: lineno, function: "create", context_line: "  line #{lineno}  ", in_app: in_app, vars: { secret: "x" } }
  end
end
