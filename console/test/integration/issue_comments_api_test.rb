# frozen_string_literal: true

require "test_helper"

# Seam 2: the Dispatcher adds a comment to an Issue, such as an Issue Run's
# cause report, with POST /api/issue_comments and a token with
# issues:comment. A comment from the Dispatcher's token, or from one of its
# child tokens, shows "Factory" with the Run's link as its author, not the
# Admin who made the token.
class IssueCommentsApiTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  DISPATCHER_SCOPES = [ "run_events:write", "issues:comment", "tokens:create", "mcp:read" ].freeze

  setup do
    @admin = create(:user, name: "Dana", email: "dana@example.com")
    @member = create(:user, name: "Mo", email: "mo@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @admin, role: :admin)
    create(:organization_user, organization: @acme, user: @member, role: :member)
    create(:organization_user, organization: @beta, user: @admin, role: :admin)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    @issue = create(:issue, project: @shop, title: "Payment failed")
    @dispatcher, @secret = ApiToken.issue!(user: @admin, name: "Dispatcher", scopes: DISPATCHER_SCOPES,
                                           organization_id: @acme.id, expires_at: 30.days.from_now)
  end

  # Factory comments

  test "the Dispatcher's token adds a comment that shows Factory with the Run's link as its author" do
    assert_difference -> { @issue.comments.count }, 1 do
      add_comment(@secret, body: "Cause: the card API timed out.\n\nSee the Run.", run_id: "r_01JD9")
    end

    assert_response :created
    comment = response.parsed_body["comment"]
    assert_equal({ "name" => "Factory", "run" => { "id" => "r_01JD9", "url" => "http://www.example.com/acme/runs/r_01JD9" } },
                 comment["author"])
    assert_match "Cause: the card API timed out.", comment["body"]
    assert_equal "http://www.example.com/acme/projects/shop/issues/#{@issue.number}#comment-#{comment['id']}", comment["url"]

    shown = issue_page_comments.sole
    assert_nil shown["user"]
    assert_equal({ "id" => "r_01JD9", "url" => "/acme/runs/r_01JD9" }, shown["run"])
    assert_includes shown["body"], "<p>Cause: the card API timed out.</p>"
    assert_includes shown["body"], "<p>See the Run.</p>"
  end

  test "a child token with issues:comment also adds a Factory comment" do
    _, child = ApiToken.issue_child!(parent: @dispatcher, name: "Run r2", scopes: [ "issues:comment" ], expires_at: 1.hour.from_now)

    add_comment(child, run_id: "r2")

    assert_response :created
    assert_equal "Factory", response.parsed_body.dig("comment", "author", "name")
    assert_equal "r2", issue_page_comments.sole.dig("run", "id")
  end

  test "the body is plain text: HTML in it shows as text" do
    add_comment(@secret, body: "<script>alert(1)</script> & <b>bold</b>", run_id: "r1")

    assert_response :created
    body = issue_page_comments.sole["body"]
    assert_includes body, "&lt;script&gt;alert(1)&lt;/script&gt; &amp; &lt;b&gt;bold&lt;/b&gt;"
    assert_not_includes body, "<script>"
  end

  test "nobody can delete a Factory comment on the Issue page, also the Admin who made the token" do
    add_comment(@secret, run_id: "r1")
    comment = @issue.comments.sole
    sign_in_as(@admin)

    delete project_issue_comment_url(@shop, @issue, comment, org_slug: "acme")

    assert Comment.exists?(comment.id)
  end

  test "MCP get_issue shows a Factory comment's author as Factory with the Run's link" do
    add_comment(@secret, body: "The cause.", run_id: "r1")
    stub_request(:get, %r{/api/#{@shop.id}/events}).to_raise(Errno::ECONNREFUSED)
    _, mcp_secret = ApiToken.issue!(user: @member, name: "Agent", scopes: [ "mcp:read" ], organization_id: @acme.id)

    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: "tools/call",
                            params: { name: "get_issue", arguments: { project: "shop", issue: @issue.number } } }.to_json,
                  headers: { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json",
                             "Authorization" => "Bearer #{mcp_secret}" }

    text = response.parsed_body.dig("result", "content", 0, "text").split("\nConsole: ").first
    comment = JSON.parse(text)["comments"].sole
    assert_equal({ "name" => "Factory", "run" => "http://www.example.com/acme/runs/r1" }, comment["author"])
    assert_equal "The cause.", comment["body"]
  end

  # A person's token with issues:comment comments as that person.

  test "a person's own token with issues:comment adds the comment as that person, and cannot link a Run" do
    _, mine = ApiToken.issue!(user: @member, name: "Mine", scopes: [ "issues:comment" ], organization_id: @acme.id)

    add_comment(mine, run_id: "r1")
    assert_response :unprocessable_content
    assert_match "Only the Dispatcher's token", response.parsed_body.dig("errors", "run_id").join

    add_comment(mine, body: "Looking at it.")
    assert_response :created
    assert_equal({ "name" => "Mo" }, response.parsed_body.dig("comment", "author"))
    assert_equal "Mo", issue_page_comments.sole.dig("user", "name")
  end

  # Refusals

  test "a token without issues:comment cannot add the comment" do
    _, mcp = ApiToken.issue!(user: @admin, name: "MCP", scopes: [ "mcp" ], organization_id: @acme.id)
    _, no_scope = ApiToken.issue!(user: @admin, name: "Laptop")
    _, child = ApiToken.issue_child!(parent: @dispatcher, name: "Run", scopes: [ "run_events:write" ], expires_at: 1.hour.from_now)

    [ mcp, no_scope, child ].each do |secret|
      assert_no_difference -> { Comment.count } do
        add_comment(secret, run_id: "r1")
      end
      assert_response :forbidden
      assert_equal "insufficient_scope", response.parsed_body["error"]
      assert_equal %(Bearer error="insufficient_scope", scope="issues:comment"), response.headers["WWW-Authenticate"]
    end
  end

  test "an unknown, revoked or expired token gets 401" do
    _, expiring = ApiToken.issue_child!(parent: @dispatcher, name: "Run", scopes: [ "issues:comment" ], expires_at: 1.hour.from_now)
    _, revoked = ApiToken.issue!(user: @admin, name: "Gone", scopes: [ "issues:comment" ], organization_id: @acme.id)
    ApiToken.find_by_secret(revoked).destroy!

    travel 2.hours do
      [ "st_unknown", revoked, expiring, nil ].each do |secret|
        assert_no_difference -> { Comment.count } do
          add_comment(secret, run_id: "r1")
        end
        assert_response :unauthorized
        assert_equal %(Bearer error="invalid_token"), response.headers["WWW-Authenticate"]
      end
    end
  end

  test "the token stops when its Admin leaves the Organization" do
    @admin.organization_users.find_by!(organization: @acme).discard!

    add_comment(@secret, run_id: "r1")

    assert_response :unauthorized
  end

  test "an Organization that requires Two-factor refuses an Admin without it" do
    @acme.update!(require_two_factor: true)

    add_comment(@secret, run_id: "r1")

    assert_response :forbidden
    assert_equal "two_factor_required", response.parsed_body["error"]
  end

  test "an Issue of another Organization, or one that does not exist, is not found" do
    theirs = create(:issue, project: create(:project, organization: @beta, slug: "shop"))

    add_comment(@secret, issue: theirs.number + 100, run_id: "r1")
    assert_response :not_found
    add_comment(@secret, project: "nope", run_id: "r1")
    assert_response :not_found
    assert_no_difference -> { theirs.comments.count } do
      add_comment(@secret, organization: "beta", issue: theirs.number, run_id: "r1")
    end
    assert_equal 0, theirs.comments.count
  end

  test "a child token with a Project limit comments only on that Project's Issues" do
    admin_project = create(:project, organization: @acme, slug: "admin")
    other = create(:issue, project: admin_project)
    _, child = ApiToken.issue_child!(parent: @dispatcher, name: "Run", scopes: [ "issues:comment", "mcp:read" ],
                                     expires_at: 1.hour.from_now, project_id: @shop.id)

    add_comment(child, project: "admin", issue: other.number, run_id: "r1")
    assert_response :not_found

    add_comment(child, run_id: "r1")
    assert_response :created
  end

  test "a Factory comment needs a body and a Run id in the Run's form" do
    [ { body: "" }, { body: "x" * (IssueCommentsController::MAX_BODY + 1) }, { body: [ "a" ] },
      { run_id: nil }, { run_id: "" }, { run_id: "../settings" }, { run_id: "r" * 65 }, { run_id: 12 } ].each do |fields|
      assert_no_difference -> { Comment.count } do
        add_comment(@secret, **{ run_id: "r1" }.merge(fields))
      end
      assert_response :unprocessable_content, fields.inspect
    end
  end

  private

  def add_comment(secret, project: "shop", issue: @issue.number, body: "The cause.", **fields)
    headers = secret ? { "Authorization" => "Bearer #{secret}" } : {}
    post issue_comments_api_url, params: { project: project, issue: issue, body: body, **fields },
                                 headers: headers, as: :json
  end

  def issue_page_comments
    sign_in_as(@member)
    get project_issue_url(@shop, @issue, org_slug: "acme"), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    response.parsed_body.dig("props", "comments")
  end
end
