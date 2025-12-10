# frozen_string_literal: true

module Api
  module V1
    class IngestController < Api::BaseController
      before_action :authenticate_project

      def store
        # payload is in params for JSON requests
        event_data = params.except(:project_id, :controller, :action, :sentry_key, :sentry_version, :sentry_client).to_unsafe_h

        EventIngestor.new(@project, event_data).call

        render plain: "", status: :ok
      end

      def envelope
        # Envelope payload is raw body
        raw_envelope = request.body.read

        # Parse envelope: header\nitem\n...
        lines = raw_envelope.split("\n")
        _header = JSON.parse(lines[0])
        item_header = JSON.parse(lines[1])
        item_data = JSON.parse(lines[2])

        if item_header["type"] == "event"
          EventIngestor.new(@project, item_data).call
        end

        render plain: "", status: :ok
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
    end
  end
end
