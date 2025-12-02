module Api
  module V1
    class IngestController < ApplicationController
      skip_before_action :authenticate_request

      before_action :authenticate_project

      def store
        # payload is in params for JSON requests
        event_data = params.except(:project_id, :controller, :action, :sentry_key, :sentry_version, :sentry_client)

        process_event(event_data)

        render json: { id: SecureRandom.uuid }, status: :ok
      end

      def envelope
        # Envelope payload is raw body
        raw_envelope = request.body.read

        # Parse envelope: header\nitem\n...
        lines = raw_envelope.split("\n")
        header = JSON.parse(lines[0])
        item_header = JSON.parse(lines[1])
        item_data = JSON.parse(lines[2])

        if item_header["type"] == "event"
          process_event(item_data)
        end

        render json: { id: SecureRandom.uuid }, status: :ok
      end

      private

      def authenticate_project
        auth_header = request.headers["X-Sentry-Auth"] || request.headers["Authorization"]

        if auth_header
          # Parse header: Sentry sentry_key=value, ...
          # Simple regex or split
          if match = auth_header.match(/sentry_key=([^,\s]+)/)
            public_key = match[1]
          end
        else
          public_key = params[:sentry_key]
        end

        unless public_key
          render json: { error: "Missing authentication" }, status: :unauthorized
          return
        end

        @project_key = ProjectKey.find_by(public_key: public_key)

        unless @project_key
          render json: { error: "Invalid project key" }, status: :unauthorized
          return
        end

        # Verify project_id matches (if provided in URL)
        if params[:project_id] && @project_key.project_id.to_s != params[:project_id].to_s
          render json: { error: "Project mismatch" }, status: :forbidden
          return
        end

        @project = @project_key.project
      end

      def process_event(data)
        # Create Issue and IssueEvent
        # This is a simplified version. Real version needs grouping logic.

        title = data["message"] || (data["exception"] && data["exception"]["values"]&.first&.fetch("type")) || "Unknown Error"
        level = data["level"] || "error"

        # Find or create issue
        # Simple grouping by title for now
        issue = @project.issues.find_or_create_by(title: title) do |i|
          i.level = level_to_int(level)
          i.status = 0 # unresolved
        end

        IssueEvent.create!(
          issue: issue,
          data: data
        )
      end

      def level_to_int(level)
        case level
        when "fatal" then 50
        when "error" then 40
        when "warning" then 30
        when "info" then 20
        when "debug" then 10
        else 40
        end
      end
    end
  end
end
