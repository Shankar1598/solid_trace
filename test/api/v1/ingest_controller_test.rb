# frozen_string_literal: true

require "test_helper"

module Api
  module V1
    class IngestControllerTest < ActionDispatch::IntegrationTest
      setup do
        @organization = create(:organization)
        @project = create(:project, organization: @organization)
        @project_key = create(:project_key, project: @project)

        @valid_payload = {
          message: "Test Error",
          culprit: "test_controller.rb",
          level: "error",
          exception: {
            values: [
              { type: "RuntimeError", value: "Something went wrong" }
            ]
          }
        }
      end

      test "store should create issue and event with valid key in params" do
        assert_difference([ "Issue.count", "Event.count" ], 1) do
          post api_ingest_store_url(project_id: @project.id),
               params: @valid_payload.merge(sentry_key: @project_key.public_key),
               as: :json
        end

        assert_response :success
      end

      test "store should create issue and event with valid key in header" do
        headers = { "X-Sentry-Auth" => "Sentry sentry_key=#{@project_key.public_key}" }

        assert_difference([ "Issue.count", "Event.count" ], 1) do
          post api_ingest_store_url(project_id: @project.id),
               params: @valid_payload,
               headers: headers,
               as: :json
        end

        assert_response :success
      end

      test "store should return 401 with invalid key" do
        post api_ingest_store_url(project_id: @project.id),
             params: @valid_payload.merge(sentry_key: "invalid_key"),
             as: :json

        assert_response :unauthorized
      end

      test "store should return 403 with project mismatch" do
        other_project = create(:project, organization: @organization)

        post api_ingest_store_url(project_id: other_project.id),
             params: @valid_payload.merge(sentry_key: @project_key.public_key),
             as: :json

        assert_response :forbidden
      end

      test "envelope should process event item" do
        header = { event_id: SecureRandom.uuid, sentry_at: Time.now.iso8601 }
        item_header = { type: "event", length: 100 } # length ignored by simple parser
        item_data = @valid_payload

        envelope_body = [
          header.to_json,
          item_header.to_json,
          item_data.to_json
        ].join("\n")

        headers = {
          "X-Sentry-Auth" => "Sentry sentry_key=#{@project_key.public_key}",
          "Content-Type" => "application/x-sentry-envelope"
        }

        assert_difference([ "Issue.count", "Event.count" ], 1) do
          post api_ingest_envelope_url(project_id: @project.id),
               params: envelope_body,
               headers: headers
        end

        assert_response :success
      end
    end
  end
end
