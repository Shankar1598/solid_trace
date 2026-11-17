# frozen_string_literal: true

require "test_helper"

module Oauth
  # The daily cleanup deletes the OAuth rows that no one can see or use any
  # more, and keeps each row until its time passes, also by one second.
  class CleanupJobTest < ActiveSupport::TestCase
    include ActiveSupport::Testing::TimeHelpers

    ISSUER = "https://console.example.com"

    setup do
      freeze_time
      @user = create(:user)
      @organization = create(:organization)
      create(:organization_user, organization: @organization, user: @user)
    end

    # Clients

    test "deletes a registered client older than 7 days with no Grant, and keeps a younger one" do
      old = client(age: 7.days + 1.second)
      young = client(age: 7.days - 1.second)

      CleanupJob.perform_now

      assert_not Client.exists?(old.id)
      assert Client.exists?(young.id)
    end

    test "deletes a published client's row older than 7 days with no Grant, as a failed first fetch leaves" do
      old = client(age: 8.days, kind: "cimd", client_id: "https://client.example.com/old.json")
      young = client(age: 6.days, kind: "cimd", client_id: "https://client.example.com/young.json")

      CleanupJob.perform_now

      assert_not Client.exists?(old.id)
      assert Client.exists?(young.id)
    end

    test "keeps an old client that has a Grant, also a revoked one" do
      with_grant = client(age: 30.days)
      grant(with_grant, first_token_at: 20.days.ago)
      with_revoked = client(age: 30.days)
      grant(with_revoked, first_token_at: 20.days.ago, revoked_at: 20.days.ago)

      CleanupJob.perform_now

      assert Client.exists?(with_grant.id)
      assert Client.exists?(with_revoked.id)
    end

    test "keeps an old client while a user is on its approval page" do
      old = client(age: 30.days)
      PendingRequest.keep!(client: old, redirect_uri: "http://localhost:4567/callback")

      CleanupJob.perform_now

      assert Client.exists?(old.id)
    end

    # Codes

    test "deletes expired codes and keeps a code until it expires" do
      grant = grant(client(age: 1.minute), first_token_at: 1.minute.ago)
      expired = code(grant, expires_at: 1.second.ago)
      live = code(grant, expires_at: 1.second.from_now)

      CleanupJob.perform_now

      assert_not Code.exists?(expired.id)
      assert Code.exists?(live.id)
    end

    # Grants

    test "deletes a Grant that has had no token after 10 minutes, with its code, and keeps a younger one" do
      old = grant(client(age: 1.hour), age: 10.minutes + 1.second)
      old_code = code(old, expires_at: 9.minutes.ago)
      young = grant(client(age: 1.hour), age: 10.minutes - 1.second)

      CleanupJob.perform_now

      assert_not Grant.exists?(old.id)
      assert_not Code.exists?(old_code.id)
      assert Grant.exists?(young.id)
    end

    test "keeps an old Grant that has had a token" do
      old = grant(client(age: 30.days), age: 30.days, first_token_at: 30.days.ago)

      CleanupJob.perform_now

      assert Grant.exists?(old.id)
    end

    # Tokens

    test "deletes tokens that expired more than 7 days ago, and keeps those that expired less long ago" do
      grant = grant(client(age: 60.days), age: 60.days, first_token_at: 60.days.ago)
      old = token(grant, expires_at: 7.days.ago - 1.second)
      young = token(grant, expires_at: 7.days.ago + 1.second)
      live = token(grant, expires_at: 1.hour.from_now)

      CleanupJob.perform_now

      assert_not Token.exists?(old.id)
      assert Token.exists?(young.id)
      assert Token.exists?(live.id)
    end

    test "deletes tokens revoked more than 7 days ago, and keeps those revoked less long ago" do
      grant = grant(client(age: 60.days), age: 60.days, first_token_at: 60.days.ago)
      old = token(grant, revoked_at: 7.days.ago - 1.second)
      young = token(grant, revoked_at: 7.days.ago + 1.second)

      CleanupJob.perform_now

      assert_not Token.exists?(old.id)
      assert Token.exists?(young.id)
    end

    test "deletes the tokens of a Grant revoked more than 7 days ago, and keeps those of a Grant revoked less long ago" do
      old = grant(client(age: 60.days), age: 60.days, first_token_at: 60.days.ago)
      old_tokens = [ token(old, expires_at: 20.days.from_now), token(old, expires_at: 1.hour.from_now) ]
      young = grant(client(age: 60.days), age: 60.days, first_token_at: 60.days.ago)
      young_token = token(young, expires_at: 20.days.from_now)
      old.revoke!
      travel 2.seconds
      young.revoke!
      travel 7.days - 1.second

      CleanupJob.perform_now

      old_tokens.each { |token| assert_not Token.exists?(token.id) }
      assert Token.exists?(young_token.id)
      assert Grant.exists?(old.id)
    end

    test "deletes an old used refresh token whose replacement is live, and keeps the replacement" do
      grant = grant(client(age: 60.days), age: 60.days, first_token_at: 60.days.ago)
      replacement = token(grant, expires_at: 20.days.from_now)
      used = token(grant, expires_at: 8.days.ago, used_at: 10.days.ago, replaced_by: replacement)

      CleanupJob.perform_now

      assert_not Token.exists?(used.id)
      assert Token.exists?(replacement.id)
    end

    # Pending requests

    test "deletes expired pending requests and keeps fresh ones" do
      kept_for = client(age: 1.hour)
      old = PendingRequest.create!(client: kept_for, redirect_uri: "http://localhost:4567/callback",
                                   created_at: PendingRequest::LASTS.ago - 1.second)
      fresh = PendingRequest.create!(client: kept_for, redirect_uri: "http://localhost:4567/callback",
                                     created_at: PendingRequest::LASTS.ago + 1.second)

      CleanupJob.perform_now

      assert_not PendingRequest.exists?(old.id)
      assert PendingRequest.exists?(fresh.id)
    end

    test "runs every day" do
      tasks = YAML.load_file(Rails.root.join("config/recurring.yml"), aliases: true)
      task = tasks.dig("production", "oauth_cleanup")

      assert_equal "Oauth::CleanupJob", task["class"]
      assert_match(/every day/, task["schedule"])
    end

    private

    def client(age:, kind: "dcr", client_id: Secret.generate)
      Client.create!(kind: kind, client_id: client_id, name: "Claude Code",
                     redirect_uris: [ "http://localhost:4567/callback" ], created_at: age.ago)
    end

    def grant(client, age: 1.minute, first_token_at: nil, revoked_at: nil)
      Grant.create!(user: @user, client: client, organization: @organization, scopes: [ "mcp" ],
                    resources: [ "#{ISSUER}/mcp" ], approved_via: "browser", approved_at: age.ago,
                    created_at: age.ago, first_token_at: first_token_at, revoked_at: revoked_at)
    end

    def code(grant, expires_at:)
      Code.create!(kind: "authorization", code_digest: Secret.digest(Secret.generate), client: grant.client,
                   grant: grant, code_challenge: "x" * 43, redirect_uri: "http://localhost:4567/callback",
                   scopes: [ "mcp" ], resource: "#{ISSUER}/mcp", expires_at: expires_at)
    end

    def token(grant, expires_at: 1.day.from_now, revoked_at: nil, used_at: nil, replaced_by: nil)
      Token.create!(kind: "refresh", token_digest: Secret.digest(Secret.generate), grant: grant,
                    expires_at: expires_at, revoked_at: revoked_at, used_at: used_at, replaced_by: replaced_by)
    end
  end
end
