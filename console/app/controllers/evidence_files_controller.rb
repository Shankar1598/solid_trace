# frozen_string_literal: true

# The Evidence files pages (Organization navigation): the list, newest
# first, and the page of one file, with its image or its video, its
# caption, who uploaded it and when, and links to its Agent session and to
# that session's Review record. Each member sees them; someone outside the
# Organization gets 404 (set_current_org), and a person who is not signed
# in signs in and comes back to the same URL. EvidenceFileBytesController
# serves the bytes.
class EvidenceFilesController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def index
    rows = @current_org.evidence_files.includes(:uploaded_by_user).newest_first.limit(EvidenceFile::LIST_LIMIT + 1).map(&:listed)
    render inertia: "EvidenceFiles/Index", props: {
      evidence_files: { rows: rows.first(EvidenceFile::LIST_LIMIT), more: rows.size > EvidenceFile::LIST_LIMIT },
    }
  end

  def show
    file = @current_org.evidence_files.find(params[:id].downcase)
    records = ReviewRecordsView.of_sessions(organization: @current_org, session_ids: [ file.agent_session_id ])
    render inertia: "EvidenceFiles/Show", props: {
      evidence_file: file.listed.merge(
        review_record: records&.dig(file.agent_session_id),
        list_path: evidence_files_path(org_slug: @current_org.slug),
      ),
    }
  end
end
