# frozen_string_literal: true

# The bytes of an Evidence file, for its page's image or video: Console
# finds the file in the Organization of the URL, which only its members can
# open (set_current_org), then streams the bytes from EventStore's query
# API with the browser's Range, so a video can seek and no file is read
# into memory. The headers keep the bytes from running as a page on
# Console's domain, and from a shared cache. The bytes of an id never
# change, so the browser keeps them, with the file's sha256 as the ETag. A
# file that Evidence Retention deleted gets 410.
#
# Live streams the answer, and runs each action in a thread of its own,
# so the pages of Evidence files are in EvidenceFilesController.
class EvidenceFileBytesController < ApplicationController
  include ActionController::Live

  before_action :set_current_org

  # The headers of a file's bytes, which a plain answer drops.
  FILE_HEADERS = %w[Content-Type Content-Range Accept-Ranges Content-Disposition ETag].freeze

  def show
    file = @current_org.evidence_files.find(params[:id].downcase)
    response.headers.merge!("X-Content-Type-Options" => "nosniff", "Content-Security-Policy" => "sandbox", "Cache-Control" => "private")
    return render plain: "Gone", status: :gone if file.deleted?

    keep_in_browser(file)
    return head :not_modified if request.if_none_match_etags.include?(response.headers["ETag"])

    stream_bytes(file)
  end

  private

  def keep_in_browser(file)
    response.headers["ETag"] = %("#{file.sha256}")
    response.headers["Cache-Control"] = "private, max-age=31536000, immutable"
  end

  # EventStore's 200 and 206 stream to the browser, and its 416 goes back
  # with its Content-Range. Any other answer, or a failure before the first
  # chunk, gets 502 with no file headers; after the first chunk, the
  # browser gets the bytes up to there.
  def stream_bytes(file)
    EventStore.evidence_content(id: file.id, range: request.headers["Range"]) do |answer|
      case answer.code.to_i
      when 200, 206 then stream_file(file, answer)
      when 416 then plain_answer("Range Not Satisfiable", :range_not_satisfiable, "Content-Range" => answer["Content-Range"])
      else raise EventStore::Unavailable, "EventStore answered #{answer.code} for Evidence file #{file.id}"
      end
    end
  rescue EventStore::Unavailable => e
    Rails.logger.error("Evidence file bytes: #{e.message}")
    plain_answer("EventStore can't read the Evidence file. Try again shortly.", :bad_gateway) unless response.committed?
  ensure
    response.stream.close
  end

  def stream_file(file, answer)
    response.status = answer.code.to_i
    response.headers["Content-Type"] = file.content_type
    response.headers["Content-Disposition"] = "inline"
    %w[Content-Range Accept-Ranges].each { |name| response.headers[name] = answer[name] if answer[name] }
    answer.read_body { |chunk| response.stream.write(chunk) }
  end

  def plain_answer(text, status, headers = {})
    FILE_HEADERS.each { |name| response.headers.delete(name) }
    response.headers.merge!("Cache-Control" => "private", **headers.compact)
    render plain: text, status: status
  end
end
