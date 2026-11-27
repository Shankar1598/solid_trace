# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Review records pages (Organization navigation): the newest Review
# record of each PR in the Organization's Agent Project, from EventStore's
# /review-records, and one record whole, from /review-records/record. Each
# member sees them; someone outside the Organization gets 404; with Agent
# sessions off, the list tells how to turn them on.
class ReviewRecordsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  SESSION = "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
  RECORD = "6f1e3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"
  EARLIER = "9e1f3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"
  PR = "https://github.com/acme/shop/pull/7"

  setup do
    @organization = create(:organization, slug: "acme")
    @admin = create(:user, name: "Ada")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user, name: "Bob")
    create(:organization_user, organization: @organization, user: @member, role: :member)
  end

  def turn_on
    AgentSessionsSettings.turn_on(organization: @organization, user: @admin, entries: [ "github.com/acme/shop" ], confirmed: "1").project
  end

  def row(id: RECORD, round: 2)
    { key: PR, id: id, session_id: SESSION, path: "#{SESSION}.jsonl", version: 1, offset: 4096, stored_at: "2026-10-02T11:30:00Z",
      pr: PR, round: round, title: "Cart page", findings: 4, fixed: 2, missed: 3, confirmations_not_given: 1 }
  end

  # A record as an agent writes it, with text that must stay text.
  def record_json
    { version: 2, pr: PR, pr_body: "## Cart page\n\nAdds a <b>cart</b> page.", round: 2, story: { started_from: "Add a <b>cart</b> page" },
      findings: [ { id: "1.F1", found_by: { kind: "reviewer", name: "tests" }, events: [ { type: "triage", decision: "fix" } ], outcome: "fixed" } ],
      confirmations: [] }.to_json
  end

  def inertia_page(path, component)
    get path, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal component, response.parsed_body["component"]
    response.parsed_body["props"]
  end

  test "a member sees the newest record of each PR" do
    project = turn_on
    asked = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/review-records")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" },
                 body: { review_records: [ row.merge(earlier: 1) ], more: false }.to_json)
    sign_in_as(@member)

    view = inertia_page(review_records_path(org_slug: "acme"), "ReviewRecords/Index")["review_records"]

    assert_requested asked
    assert view["on"]
    assert view.dig("event_store", "available")
    listed = view["rows"].sole
    assert_equal [ PR, 2, 4, 2, 3, 1, 1, SESSION ],
      listed.values_at("pr", "round", "findings", "fixed", "missed", "confirmations_not_given", "earlier", "session_id")
  end

  test "a member opens the newest record of a PR by its key, with its JSON and the rounds of the PR" do
    project = turn_on
    asked = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/review-records/record")
      .with(body: { key: PR }.to_json)
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: {
        review_record: row, record: record_json,
        rounds: [ { id: RECORD, session_id: SESSION, round: 2 }, { id: EARLIER, session_id: SESSION, round: 1 } ],
      }.to_json)
    sign_in_as(@member)

    view = inertia_page(review_record_path(org_slug: "acme", key: PR), "ReviewRecords/Show")["review_record"]

    assert_requested asked
    assert_equal RECORD, view.dig("review_record", "id")
    assert_equal "Add a <b>cart</b> page", view.dig("record", "story", "started_from")
    assert_equal "## Cart page\n\nAdds a <b>cart</b> page.", view.dig("record", "pr_body"), "the PR body as the agent wrote it"
    assert_equal "fixed", view.dig("record", "findings", 0, "outcome")
    assert_equal [ 2, 1 ], view["rounds"].map { |r| r["round"] }
    assert_equal "/acme/review_records", view["list_path"]
    assert_equal [], view["evidence_files"]
  end

  test "a record's page lists the Evidence files of the Agent sessions of all its PR's rounds, newest first, and no other" do
    project = turn_on
    first_round = "1c6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
    other_session = "2c6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
    other_org = create(:organization, slug: "globex")
    files = { "1f000000-0000-4f6a-8c0e-3d5b7f9a1c4e" => [ @organization, SESSION, 2.minutes.ago, "video/webm" ],
              "2f000000-0000-4f6a-8c0e-3d5b7f9a1c4e" => [ @organization, first_round, 1.minute.ago, "image/png" ],
              "3f000000-0000-4f6a-8c0e-3d5b7f9a1c4e" => [ @organization, first_round, 1.hour.ago, "image/png" ],
              "4f000000-0000-4f6a-8c0e-3d5b7f9a1c4e" => [ @organization, other_session, Time.current, "image/png" ],
              "5f000000-0000-4f6a-8c0e-3d5b7f9a1c4e" => [ other_org, SESSION, Time.current, "image/png" ] }
    files.each do |id, (organization, session, at, type)|
      EvidenceFile.insert!({ id: id, organization_id: organization.id, agent_session_id: session, sha256: id, caption: "Cart page",
                             content_type: type, byte_size: 1000, storage_key: "evidence/#{id}", created_at: at })
    end
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/review-records/record")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: {
        review_record: row, record: record_json,
        rounds: [ { id: RECORD, session_id: SESSION, round: 2 }, { id: EARLIER, session_id: first_round.upcase, round: 1 } ],
      }.to_json)
    sign_in_as(@member)

    view = inertia_page(review_record_path(org_slug: "acme", id: RECORD), "ReviewRecords/Show")["review_record"]

    assert_equal [ [ "2f000000-0000-4f6a-8c0e-3d5b7f9a1c4e", first_round ], [ "1f000000-0000-4f6a-8c0e-3d5b7f9a1c4e", SESSION ],
                   [ "3f000000-0000-4f6a-8c0e-3d5b7f9a1c4e", first_round ] ],
      view["evidence_files"].map { |f| f.values_at("id", "agent_session_id") }
    assert_equal [ "Cart page", "video/webm", nil ], view["evidence_files"][1].values_at("caption", "content_type", "uploaded_by")
  end

  test "an earlier round opens by its id" do
    project = turn_on
    asked = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/review-records/record")
      .with(body: { id: EARLIER }.to_json)
      .to_return(status: 200, headers: { "Content-Type" => "application/json" },
                 body: { review_record: row(id: EARLIER, round: 1), record: record_json, rounds: [] }.to_json)
    sign_in_as(@member)

    view = inertia_page(review_record_path(org_slug: "acme", id: EARLIER), "ReviewRecords/Show")["review_record"]

    assert_requested asked
    assert_equal 1, view.dig("review_record", "round")
  end

  test "a record that EventStore does not have, and a link with no key or id or with both, get 404" do
    project = turn_on
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/review-records/record")
      .to_return(status: 404, body: { error: "no such Review record" }.to_json)
    sign_in_as(@member)

    get review_record_path(org_slug: "acme", key: "https://github.com/acme/shop/pull/8")
    assert_response :not_found
    get review_record_path(org_slug: "acme")
    assert_response :not_found
    get review_record_path(org_slug: "acme", key: PR, id: RECORD)
    assert_response :not_found
    get review_record_path(org_slug: "acme", id: "round-1")
    assert_response :not_found
    assert_requested :post, "#{EventStore.base_url}/api/#{project.id}/review-records/record", times: 1
  end

  test "when EventStore cannot answer, the list says why and has no rows" do
    project = turn_on
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/review-records").to_return(status: 500, body: "boom")
    sign_in_as(@member)

    view = inertia_page(review_records_path(org_slug: "acme"), "ReviewRecords/Index")["review_records"]

    assert_not view.dig("event_store", "available")
    assert_match "Review records", view.dig("event_store", "error")
    assert_equal [], view["rows"]
  end

  test "with Agent sessions off, the list tells how to turn them on, a record is not found, and nothing is read" do
    sign_in_as(@member)

    view = inertia_page(review_records_path(org_slug: "acme"), "ReviewRecords/Index")["review_records"]

    assert_not view["on"]
    assert_not view["can_turn_on"]
    assert_equal "/acme/settings/agent_sessions", view["settings_path"]
    get review_record_path(org_slug: "acme", key: PR)
    assert_response :not_found
    assert_not_requested :post, /review-records/

    sign_in_as(@admin)
    assert inertia_page(review_records_path(org_slug: "acme"), "ReviewRecords/Index").dig("review_records", "can_turn_on")
  end

  test "someone outside the Organization gets 404" do
    turn_on
    sign_in_as(create(:user))

    get review_records_path(org_slug: "acme")
    assert_response :not_found
    get review_record_path(org_slug: "acme", key: PR)
    assert_response :not_found
    assert_not_requested :post, /review-records/
  end

  test "each Organization reads the Review records of its own Agent Project only" do
    acme = stub_request(:post, "#{EventStore.base_url}/api/#{turn_on.id}/review-records")
    other = create(:organization, slug: "other")
    other_admin = create(:user)
    create(:organization_user, organization: other, user: other_admin, role: :admin)
    other_project = AgentSessionsSettings.turn_on(organization: other, user: other_admin, entries: [ "github.com/other/app" ], confirmed: "1").project
    theirs = stub_request(:post, "#{EventStore.base_url}/api/#{other_project.id}/review-records")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { review_records: [], more: false }.to_json)
    sign_in_as(other_admin)

    inertia_page(review_records_path(org_slug: "other"), "ReviewRecords/Index")

    assert_requested theirs
    assert_not_requested acme
  end
end
