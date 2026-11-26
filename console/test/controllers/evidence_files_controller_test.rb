# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Evidence files pages and their bytes: the rows are in Console's
# evidence_files table, which EventStore writes; the bytes come from
# EventStore's /evidence/:id/content, streamed with the browser's Range.
# Each member of the file's Organization sees them; anyone else gets 404,
# and a person who is not signed in signs in and comes back.
class EvidenceFilesControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  SESSION = "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
  SHOT = "6f1e3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"
  VIDEO = "9e1f3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"
  OTHER = "1a2b3c4d-2b4d-4f6a-8c0e-3d5b7f9a1c4e"

  setup do
    @organization = create(:organization, slug: "acme")
    @admin = create(:user, name: "Ada")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user, name: "Bob")
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @other_org = create(:organization, slug: "globex")
    @outsider = create(:user, name: "Eve")
    create(:organization_user, organization: @other_org, user: @outsider, role: :admin)

    evidence(SHOT, caption: "Cart page", created_at: Time.utc(2026, 11, 26, 10, 0, 0))
    evidence(VIDEO, content_type: "video/webm", byte_size: 1000, created_at: Time.utc(2026, 11, 26, 11, 0, 0))
    evidence(OTHER, organization: @other_org, created_at: Time.utc(2026, 11, 26, 12, 0, 0))
    stub_request(:post, %r{/api/\d+/review-records/sessions\z})
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { sessions: {} }.to_json)
  end

  # A row as EventStore writes it.
  def evidence(id, organization: @organization, content_type: "image/png", byte_size: 84, caption: nil, created_at: Time.current, deleted_at: nil)
    EvidenceFile.insert!({
      id: id, organization_id: organization.id, agent_session_id: SESSION, sha256: SecureRandom.hex(32), caption: caption,
      content_type: content_type, byte_size: byte_size, storage_key: "evidence/org=#{organization.id}/#{id}.png",
      uploaded_by_user_id: @admin.id, created_at: created_at, deleted_at: deleted_at,
    })
  end

  def inertia_page(path, component)
    get path, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal component, response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def content_url(id)
    "#{EventStore.base_url}/evidence/#{id}/content"
  end

  test "a member sees the Organization's Evidence files, the newest first" do
    sign_in_as(@member)

    view = inertia_page(evidence_files_path(org_slug: "acme"), "EvidenceFiles/Index")["evidence_files"]

    assert_equal [ VIDEO, SHOT ], view["rows"].map { |row| row["id"] }
    assert_equal [ "Cart page", "image/png", 84, SESSION, "Ada", nil ],
      view["rows"].last.values_at("caption", "content_type", "byte_size", "agent_session_id", "uploaded_by", "deleted_at")
    assert_not view["more"]
  end

  test "a member opens a file's page, with the Review record of its Agent session" do
    project = AgentSessionsSettings.turn_on(organization: @organization, user: @admin, entries: [ "github.com/acme/shop" ], confirmed: "1").project
    asked = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/review-records/sessions")
      .with(body: hash_including(session_ids: [ SESSION ]))
      .to_return(status: 200, headers: { "Content-Type" => "application/json" },
                 body: { sessions: { SESSION => { id: "r1", pr: "https://github.com/acme/shop/pull/7", round: 2 } } }.to_json)
    sign_in_as(@member)

    view = inertia_page(evidence_file_path(org_slug: "acme", id: SHOT.upcase), "EvidenceFiles/Show")["evidence_file"]

    assert_requested asked
    assert_equal [ SHOT, "Cart page", SESSION, "Ada", "/acme/evidence" ], view.values_at("id", "caption", "agent_session_id", "uploaded_by", "list_path")
    assert_equal({ "id" => "r1", "pr" => "https://github.com/acme/shop/pull/7", "round" => 2 }, view["review_record"])
  end

  test "a person who is not signed in signs in, then comes back to the file's page" do
    get evidence_file_path(org_slug: "acme", id: SHOT)
    assert_redirected_to login_path

    post login_url, params: { email: @member.email, password: "password" }

    assert_redirected_to evidence_file_url(org_slug: "acme", id: SHOT)
  end

  test "someone who is not a member gets 404 for the page and the bytes, and EventStore is not asked" do
    bytes = stub_request(:get, %r{/evidence/.*/content})
    sign_in_as(@outsider)

    get evidence_file_path(org_slug: "acme", id: SHOT)
    assert_response :not_found
    get evidence_file_bytes_path(org_slug: "acme", id: SHOT)
    assert_response :not_found
    get evidence_files_path(org_slug: "acme")
    assert_response :not_found

    assert_not_requested bytes
  end

  test "a file of another Organization is 404 under this one, also to a member of both" do
    create(:organization_user, organization: @other_org, user: @member, role: :member)
    bytes = stub_request(:get, %r{/evidence/.*/content})
    sign_in_as(@member)

    get evidence_file_path(org_slug: "acme", id: OTHER)
    assert_response :not_found
    get evidence_file_bytes_path(org_slug: "acme", id: OTHER)
    assert_response :not_found
    assert_equal [ VIDEO, SHOT ], inertia_page(evidence_files_path(org_slug: "acme"), "EvidenceFiles/Index").dig("evidence_files", "rows").map { |row| row["id"] }

    assert_not_requested bytes
  end

  test "the bytes have the stored type and the headers that keep them from running as a page" do
    stub_request(:get, content_url(SHOT)).with { |request| !request.headers.key?("Range") }
      .to_return(status: 200, headers: { "Content-Type" => "text/html", "Content-Length" => "84", "Accept-Ranges" => "bytes" }, body: "p" * 84)
    sign_in_as(@member)

    get evidence_file_bytes_path(org_slug: "acme", id: SHOT)

    assert_response :ok
    assert_equal "p" * 84, response.body
    assert_equal "image/png", response.media_type
    assert_equal "nosniff", response.headers["X-Content-Type-Options"]
    assert_equal "sandbox", response.headers["Content-Security-Policy"]
    assert_equal "inline", response.headers["Content-Disposition"]
    assert_equal "private", response.headers["Cache-Control"]
    assert_equal "bytes", response.headers["Accept-Ranges"]
  end

  test "the browser's Range goes to EventStore, and its 206 comes back with its range" do
    asked = stub_request(:get, content_url(VIDEO)).with(headers: { "Range" => "bytes=100-199" })
      .to_return(status: 206, headers: { "Content-Range" => "bytes 100-199/1000", "Content-Type" => "video/webm" }, body: "v" * 100)
    sign_in_as(@member)

    get evidence_file_bytes_path(org_slug: "acme", id: VIDEO), headers: { "Range" => "bytes=100-199" }

    assert_requested asked
    assert_response :partial_content
    assert_equal "bytes 100-199/1000", response.headers["Content-Range"]
    assert_equal "video/webm", response.media_type
    assert_equal "v" * 100, response.body
  end

  test "a range outside the file gets EventStore's 416" do
    stub_request(:get, content_url(VIDEO))
      .to_return(status: 416, headers: { "Content-Range" => "bytes */1000", "Content-Type" => "application/json" }, body: { error: "The range is outside the file" }.to_json)
    sign_in_as(@member)

    get evidence_file_bytes_path(org_slug: "acme", id: VIDEO), headers: { "Range" => "bytes=5000-" }

    assert_response :range_not_satisfiable
    assert_equal "bytes */1000", response.headers["Content-Range"]
  end

  test "a deleted file's page says when, and its bytes get 410 without a read from EventStore" do
    deleted = "2c3d4e5f-2b4d-4f6a-8c0e-3d5b7f9a1c4e"
    evidence(deleted, created_at: Time.utc(2026, 8, 1), deleted_at: Time.utc(2026, 10, 30, 3, 0, 0))
    bytes = stub_request(:get, content_url(deleted))
    sign_in_as(@member)

    assert_equal "2026-10-30T03:00:00.000Z", inertia_page(evidence_file_path(org_slug: "acme", id: deleted), "EvidenceFiles/Show").dig("evidence_file", "deleted_at")
    get evidence_file_bytes_path(org_slug: "acme", id: deleted)

    assert_response :gone
    assert_equal "This evidence was deleted on October 30, 2026.", response.body
    assert_not_requested bytes
  end

  test "the bytes get 502 when EventStore cannot be reached" do
    stub_request(:get, content_url(SHOT)).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@member)

    get evidence_file_bytes_path(org_slug: "acme", id: SHOT)

    assert_response :bad_gateway
    assert_equal "EventStore can't read the Evidence file. Try again shortly.", response.body
  end
end
