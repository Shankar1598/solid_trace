# frozen_string_literal: true

class EvidenceFilesController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def index
    rows = @current_org.evidence_files.limit(EvidenceFile::LIST_LIMIT + 1).list_rows
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
        review_record_known: !records.nil?,
        list_path: evidence_files_path(org_slug: @current_org.slug),
      ),
    }
  end
end
