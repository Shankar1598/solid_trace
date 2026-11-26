# frozen_string_literal: true

# The bytes of an Evidence file, for its page's image or video: Console
# finds the file in the Organization of the URL, which only its members can
# open (set_current_org), then streams the bytes from EventStore's query
# API with the browser's Range, so a video can seek and no file is read
# into memory. The headers keep the bytes from running as a page on
# Console's domain, and from a shared cache. A file that Evidence Retention
# deleted gets 410.
#
# Live streams the answer, and runs each action in a thread of its own,
# so the pages of Evidence files are in EvidenceFilesController.
class EvidenceFileBytesController < ApplicationController
  include ActionController::Live

  before_action :set_current_org

  # The headers of EventStore's answer that go to the browser as they are.
  ANSWER_HEADERS = %w[Content-Range Accept-Ranges].freeze

  def show
    file = @current_org.evidence_files.find(params[:id].downcase)
    response.headers.merge!("X-Content-Type-Options" => "nosniff", "Content-Security-Policy" => "sandbox", "Cache-Control" => "private")
    return render plain: "This evidence was deleted on #{file.deleted_at.to_date.to_fs(:long)}.", status: :gone if file.deleted?

    stream_bytes(file)
  end

  private

  # A failure before the first chunk gets 502; after it, the browser gets
  # the bytes up to there.
  def stream_bytes(file)
    EventStore.evidence_content(id: file.id, range: request.headers["Range"]) do |answer|
      response.status = answer.code.to_i
      ANSWER_HEADERS.each { |name| response.headers[name] = answer[name] if answer[name] }
      response.headers["Content-Type"] = answer.is_a?(Net::HTTPSuccess) ? file.content_type : answer["Content-Type"]
      response.headers["Content-Disposition"] = "inline"
      answer.read_body { |chunk| response.stream.write(chunk) }
    end
  rescue EventStore::Unavailable => e
    Rails.logger.error("Evidence file bytes: #{e.message}")
    render plain: "EventStore can't read the Evidence file. Try again shortly.", status: :bad_gateway unless response.committed?
  ensure
    response.stream.close
  end
end
