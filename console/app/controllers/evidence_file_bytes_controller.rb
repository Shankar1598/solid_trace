# frozen_string_literal: true

class EvidenceFileBytesController < ApplicationController
  before_action :open_file_as_full_page
  before_action :set_current_org

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

  def stream_bytes(file)
    stream = EvidenceFileStream.open(id: file.id, range: request.headers["Range"])
    response.status = stream.status
    response.headers.merge!("Content-Type" => file.content_type, "Content-Disposition" => "inline", **stream.headers)
    self.response_body = stream
  rescue EvidenceFileStream::OutsideTheFile => e
    plain_answer("Range Not Satisfiable", :range_not_satisfiable, "Content-Range" => e.content_range)
  rescue EvidenceFileStream::Busy
    plain_answer("Too many Evidence files are loading at once. Try again shortly.", :service_unavailable, "Retry-After" => "1")
  rescue EventStore::Unavailable => e
    Rails.logger.error("Evidence file bytes: #{e.message}")
    plain_answer("EventStore can't read the Evidence file. Try again shortly.", :bad_gateway)
  end

  def plain_answer(text, status, headers = {})
    FILE_HEADERS.each { |name| response.headers.delete(name) }
    response.headers.merge!("Cache-Control" => "private", **headers.compact)
    render plain: text, status: status
  end
end
