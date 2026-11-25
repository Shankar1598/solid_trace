# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# list_review_records and get_review_record: the newest Review record of
# each PR in an Organization's Agent Project, as the Review records page
# lists them, and one record whole, as the Review record page shows it.
# EventStore's answers are stubbed in the shape of its POST
# /api/:project_id/review-records and /review-records/record.
class McpReviewRecordToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  SESSION = "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
  RECORD = "6f1e3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"
  EARLIER = "9e1f3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"
  PR = "https://github.com/acme/shop/pull/7"

  setup do
    @admin = create(:user, name: "Ada")
    @member = create(:user, name: "Bob")
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @admin, role: :admin)
    create(:organization_user, organization: @acme, user: @member, role: :member)
    @agent = create(:project, organization: @acme, name: "Agent sessions", slug: "agent-sessions", agent: true)
    create(:organization, slug: "theirs")
  end

  # list_review_records

  test "list_review_records lists the newest record of each PR with its counts and a link to its page" do
    asked = stub_list(body: { limit: 50 }, rows: [ row.merge(earlier: 1), row(key: SESSION, pr: "", round: 1).merge(earlier: 0) ])

    got = call_tool(:list_review_records, user: @member)

    assert_not got.error?
    assert_requested asked
    pr, no_pr = got.json["review_records"]
    assert_equal [ PR, PR, 2, 4, 2, 3, 1, 1, SESSION ],
      pr.values_at("key", "pr", "round", "findings", "fixed", "missed", "confirmations_not_given", "earlier", "session_id")
    assert_equal "http://console.test/acme/review_records/record?key=https%3A%2F%2Fgithub.com%2Facme%2Fshop%2Fpull%2F7", pr["link"]
    assert_equal [ SESSION, "" ], no_pr.values_at("key", "pr")
    assert_not pr.key?("path"), "the place of the line is get_review_record's"
    assert_equal false, got.json["more"]
    assert_equal "http://console.test/acme/review_records", got.link
  end

  test "list_review_records takes a limit, and refuses one out of its range" do
    asked = stub_list(body: { limit: 5 }, rows: [], more: true)

    got = call_tool(:list_review_records, user: @member, limit: 5)

    assert_requested asked
    assert got.json["more"]
    [ 0, 201 ].each do |limit|
      server = McpServer.build(user: @member, request_url: CONSOLE_URL)
      response = server.handle({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "list_review_records", arguments: { limit: limit } } })
      assert response[:error] || response.dig(:result, :isError), "limit #{limit}"
    end
  end

  test "list_review_records says so when EventStore cannot answer" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/review-records").to_return(status: 500, body: "boom")

    got = call_tool(:list_review_records, user: @member)

    assert got.error?
    assert_match "EventStore can't read the Review records", got.text
  end

  # get_review_record

  test "get_review_record answers the record byte for byte, with its row, the rounds of its PR and a link" do
    record = record_json
    asked = stub_record(body: { key: PR }, record: record)

    got = call_tool(:get_review_record, user: @member, pr: PR)

    assert_not got.error?
    assert_requested asked
    assert_equal [ RECORD, SESSION, "#{SESSION}.jsonl", 4096 ], got.json["review_record"].values_at("id", "session_id", "path", "offset")
    assert_equal [ 2, 1 ], got.json["rounds"].pluck("round")
    assert_equal record, got.body[/"record":(.*),"rounds":/m, 1], "the record as the agent wrote it, not made again"
    assert_equal "Add a <b>cart</b> page…", got.json.dig("record", "story", "started_from")
    assert_equal "http://console.test/acme/review_records/record?key=https%3A%2F%2Fgithub.com%2Facme%2Fshop%2Fpull%2F7", got.link
  end

  test "get_review_record reads one record by its id" do
    asked = stub_record(body: { id: EARLIER }, record: record_json)

    got = call_tool(:get_review_record, user: @member, id: EARLIER)

    assert_not got.error?
    assert_requested asked
    assert_equal "http://console.test/acme/review_records/record?id=#{EARLIER}", got.link
  end

  test "get_review_record says when the Agent Project has no such record, and asks for pr or id, not both" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/review-records/record").to_return(status: 404, body: { error: "no such Review record" }.to_json)

    got = call_tool(:get_review_record, user: @member, pr: "https://github.com/acme/shop/pull/8")

    assert got.error?
    assert_match "No Review record https://github.com/acme/shop/pull/8 in acme. list_review_records lists them.", got.text
    assert_match "No Review record round-1 in acme", call_tool(:get_review_record, user: @member, id: "round-1").text
    [ {}, { pr: PR, id: RECORD }, { pr: " " } ].each do |arguments|
      assert_match "Give pr or id, not both", call_tool(:get_review_record, user: @member, **arguments).text
    end
    assert_requested :post, "#{EventStore.base_url}/api/#{@agent.id}/review-records/record", times: 1
  end

  test "get_review_record says so when EventStore cannot answer" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/review-records/record").to_return(status: 500, body: "boom")

    got = call_tool(:get_review_record, user: @member, pr: PR)

    assert got.error?
    assert_match "Try again shortly", got.text
  end

  # Both

  test "a user who is not a member of the Organization gets an error, and EventStore is not asked" do
    outsider = create(:user)
    create(:organization_user, organization: Organization.find_by!(slug: "theirs"), user: outsider)

    [ call_tool(:list_review_records, user: outsider, organization: "acme"),
      call_tool(:get_review_record, user: outsider, organization: "acme", pr: PR) ].each do |got|
      assert got.error?
      assert_match "No Organization acme among yours", got.text
    end
    assert_not_requested :post, %r{/review-records}
  end

  test "both tools say so when the Organization has no Agent Project" do
    @agent.destroy!

    [ call_tool(:list_review_records, user: @member), call_tool(:get_review_record, user: @member, pr: PR) ].each do |got|
      assert got.error?
      assert_match "Agent sessions are off in acme", got.text
    end
  end

  test "the server lists both tools, and its instructions name them" do
    assert_includes McpServer::INSTRUCTIONS, "list_review_records"
    assert_includes McpServer::INSTRUCTIONS, "get_review_record"
  end

  private

  def row(key: PR, pr: PR, round: 2, id: RECORD)
    { key: key, id: id, session_id: SESSION, path: "#{SESSION}.jsonl", version: 1, offset: 4096, stored_at: "2026-10-02T11:30:00Z",
      pr: pr, round: round, title: "Cart page", findings: 4, fixed: 2, missed: 3, confirmations_not_given: 1 }
  end

  # A record as an agent writes it: spaces and an order that JSON.generate
  # would not keep, a character it would not escape the same way, and HTML.
  def record_json
    %({ "version": 2, "round": 2, "pr": "#{PR}",\n  "story": {"started_from": "Add a <b>cart</b> page\\u2026"}, "findings": [] })
  end

  def stub_list(body:, rows:, more: false)
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/review-records")
      .with(body: body.to_json)
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { review_records: rows, more: more }.to_json)
  end

  def stub_record(body:, record:)
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/review-records/record")
      .with(body: body.to_json)
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: {
        review_record: row, record: record,
        rounds: [ { id: RECORD, session_id: SESSION, round: 2 }, { id: EARLIER, session_id: SESSION, round: 1 } ],
      }.to_json)
  end
end
