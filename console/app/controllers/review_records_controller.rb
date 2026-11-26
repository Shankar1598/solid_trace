# frozen_string_literal: true

# The Review records page (Organization navigation): the newest Review
# record of each PR that the Agent sessions of the Organization's Agent
# Project hold, and the page of one record. ReviewRecordsView and
# ReviewRecordView build them. Each member sees them, as each member reads
# the Agent sessions; someone outside the Organization gets 404
# (set_current_org). With Agent sessions off, the list tells how to turn
# them on.
class ReviewRecordsController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def index
    view = ReviewRecordsView.new(organization: @current_org)
    props = view.to_h.merge(settings_path: agent_session_settings_path(org_slug: @current_org.slug))
    props[:can_turn_on] = admin? unless props[:on]
    render inertia: "ReviewRecords/Index", props: { review_records: props }
  end

  # One record: the newest of the PR of key, or the record with id, with
  # the rounds of its PR and the Evidence files of its Agent session. 404
  # when the Organization has no Agent Project or EventStore has no such
  # record.
  def show
    view = ReviewRecordView.new(organization: @current_org, key: params[:key], id: params[:id])
    raise ActiveRecord::RecordNotFound unless view.found?

    render inertia: "ReviewRecords/Show", props: {
      review_record: view.to_h.merge(
        list_path: review_records_path(org_slug: @current_org.slug),
        evidence_files: EvidenceFile.of_sessions(@current_org, Array(view.review_record&.dig("session_id"))),
      ),
    }
  end

  private

  def admin?
    @current_org.organization_users.find_by(user: current_user)&.admin? || false
  end
end
