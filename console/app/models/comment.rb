# frozen_string_literal: true

# A comment on an Issue. Its author is a member (organization_user), or
# "Factory" with a Run (run_id) for a comment that the Dispatcher adds,
# such as an Issue Run's cause report (IssueCommentsController). A Factory
# comment has no member, so nobody deletes it as their own.
class Comment < ApplicationRecord
  # The author of a Factory comment, which the Issue page and MCP show.
  FACTORY = "Factory"
  # A Run's id, as the Run page's route takes it.
  RUN_ID = /\A[A-Za-z0-9_-]{1,64}\z/

  belongs_to :issue
  belongs_to :organization_user, optional: true
  has_one :user, through: :organization_user
  has_one :organization, through: :issue

  has_rich_text :content
  validates :content, presence: true
  validates :run_id, format: { with: RUN_ID, message: "must be a Run's id: letters, digits, - and _ (at most 64)" },
                     allow_nil: true

  validate :one_author
  validate :organization_user_must_match_comment_organization

  # Plain text as a comment's rich text: escaped, in paragraphs and lines.
  def self.html_of(text)
    ApplicationController.helpers.simple_format(ERB::Util.html_escape(text))
  end

  def factory?
    run_id.present?
  end

  # The path of the Run page of a Factory comment's Run; nil for a member's.
  def run_path
    Rails.application.routes.url_helpers.run_path(run_id, org_slug: issue.project.organization.slug) if factory?
  end

  private

  def one_author
    if organization_user.blank? && run_id.nil?
      errors.add(:organization_user, "must exist")
    elsif organization_user.present? && !run_id.nil?
      errors.add(:run_id, "is only for a Factory comment, which has no member as its author")
    end
  end

  def organization_user_must_match_comment_organization
    return if organization_user.blank? || issue.blank?

    return if organization_user.organization_id == organization.id

    errors.add(:organization_user, "must belong to the same organization as the issue")
  end
end
