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

  test "get_review_record answers the record with its first 30 findings, the number of findings, its row, the rounds of its PR and a link" do
    record = record_json(findings: 75)
    asked = stub_record(body: { key: PR }, record: record)

    got = call_tool(:get_review_record, user: @member, pr: PR)

    assert_not got.error?
    assert_requested asked
    assert_equal [ RECORD, SESSION, "#{SESSION}.jsonl", 4096 ], got.json["review_record"].values_at("id", "session_id", "path", "offset")
    assert_equal [ 2, 1 ], got.json["rounds"].pluck("round")
    whole = JSON.parse(record)
    assert_equal whole.merge("findings" => whole["findings"].first(30)), got.json["record"], "all of the record but the findings after the page"
    assert_equal({ "findings_total" => 75, "next_findings_from" => 30 }, got.json.slice("findings_total", "next_findings_from"))
    assert_equal "http://console.test/acme/review_records/record?key=https%3A%2F%2Fgithub.com%2Facme%2Fshop%2Fpull%2F7", got.link
  end

  test "get_review_record answers the page of findings that findings_from and findings_limit give" do
    stub_record(body: { key: PR }, record: record_json(findings: 75))

    got = call_tool(:get_review_record, user: @member, pr: PR, findings_from: 30)

    assert_equal (31..60).map { |n| "F#{n}" }, got.json.dig("record", "findings").pluck("id")
    assert_equal({ "findings_total" => 75, "next_findings_from" => 60 }, got.json.slice("findings_total", "next_findings_from"))
    got = call_tool(:get_review_record, user: @member, pr: PR, findings_from: 10, findings_limit: 5)
    assert_equal %w[F11 F12 F13 F14 F15], got.json.dig("record", "findings").pluck("id")
    assert_equal 15, got.json["next_findings_from"]
  end

  test "the last page of findings has a next_findings_from of null, and a page after it has no findings" do
    stub_record(body: { key: PR }, record: record_json(findings: 75))

    [ [ { findings_from: 60 }, (61..75) ], [ { findings_limit: 100 }, (1..75) ], [ { findings_from: 70, findings_limit: 5 }, (71..75) ],
      [ { findings_from: 75 }, [] ], [ { findings_from: 500 }, [] ] ].each do |arguments, numbers|
      got = call_tool(:get_review_record, user: @member, pr: PR, **arguments)

      assert_equal numbers.map { |n| "F#{n}" }, got.json.dig("record", "findings").pluck("id"), arguments.inspect
      assert_equal({ "findings_total" => 75, "next_findings_from" => nil }, got.json.slice("findings_total", "next_findings_from"), arguments.inspect)
    end
  end

  test "get_review_record refuses a findings_from or a findings_limit out of its range, and EventStore is not asked" do
    [ { findings_from: -1 }, { findings_limit: 0 }, { findings_limit: 101 }, { findings_from: "30" }, { findings_limit: 2.5 } ].each do |arguments|
      got = call_tool(:get_review_record, user: @member, pr: PR, **arguments)

      assert got.error?, arguments.inspect
      assert_match arguments.keys.sole.to_s, got.text
    end
    assert_not_requested :post, %r{/review-records}
  end

  # Claude Code cuts a tool's answer past 25,000 tokens, which is 100,000
  # characters at 4 characters a token.
  test "the first page of a record of 200 large findings is less than 100,000 characters" do
    record = JSON.generate(JSON.parse(record_json).merge("findings" => Array.new(200) { |i| large_finding(i + 1) }))
    assert_operator record.size, :>, 400_000
    stub_record(body: { key: PR }, record: record)

    got = call_tool(:get_review_record, user: @member, pr: PR)

    assert_not got.error?
    assert_equal 30, got.json.dig("record", "findings").size
    assert_operator got.text.size, :<, 100_000
  end

  test "the first page of 200 large findings and a PR body of 65,536 characters is less than 100,000 characters, and the next page has no PR body" do
    body = "A line of the PR body.\n" * 2850
    assert_operator body.size, :>=, 65_536
    body = body.first(65_536)
    record = JSON.generate(JSON.parse(record_json).merge("pr_body" => body, "findings" => Array.new(200) { |i| large_finding(i + 1) }))
    stub_record(body: { key: PR }, record: record)

    first = call_tool(:get_review_record, user: @member, pr: PR)
    second = call_tool(:get_review_record, user: @member, pr: PR, findings_from: 30)

    assert_operator first.text.size, :<, 100_000
    assert_equal body.first(16_000), first.json.dig("record", "pr_body")
    assert_equal({ "pr_body_cut" => true, "pr_body_chars" => 65_536 }, first.json.slice("pr_body_cut", "pr_body_chars"))
    assert_not second.json["record"].key?("pr_body")
    assert_not second.json.key?("pr_body_cut")
  end

  test "a PR body of 16,000 characters or less comes whole on the first page, with no pr_body_cut" do
    stub_record(body: { key: PR }, record: record_json(findings: 75))

    got = call_tool(:get_review_record, user: @member, pr: PR)

    assert_equal "## Cart page\n\nAdds a cart page.", got.json.dig("record", "pr_body")
    assert_not got.json.key?("pr_body_cut")
    assert_not got.json.key?("pr_body_chars")
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

  # A record as an agent writes it, with HTML in its text, and findings F1,
  # F2 and on.
  def record_json(findings: 0)
    JSON.pretty_generate({
      version: 2, sessions: [ { id: SESSION, title: "Cart page" } ], pr: PR, pr_body: "## Cart page\n\nAdds a cart page.", round: 2,
      story: { started_from: "Add a <b>cart</b> page…" },
      reviews: [ { run: 1, skill: "code-review", stage: "step", step: "cart page" } ],
      findings: Array.new(findings) { |i| { id: "F#{i + 1}", found_by: { kind: "reviewer", name: "tests" }, title: "Finding #{i + 1}", outcome: "fixed" } },
      confirmations: [ { id: "C1", run: 1, ask: "Confirm the deploy order.", given: true } ],
    })
  end

  # A large finding: about 2,000 characters of JSON, about twice the mean
  # of the findings of a 145 KB record.
  def large_finding(number)
    why = "The cart page adds the line totals before it applies the discount, so a cart with a coupon shows a total that is too high. "
    { id: "F#{number}", block_id: "B#{number}", run: 1, found_by: { kind: "reviewer", name: "bugs-and-spec" }, kind: "bug", severity: "high",
      category: "correctness", file: "app/cart/total.rb", line: 42, title: "The total skips the discount on a cart with a coupon",
      scenario: why * 10, events: [ { type: "found" }, { type: "triage", decision: "fix", why: why * 3 }, { type: "verify", verdict: "CONFIRMED" },
                                    { type: "fix", files: [ "app/cart/total.rb" ] } ],
      outcome: "fixed", confidence: "high" }
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
