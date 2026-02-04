# frozen_string_literal: true

class Comment < ApplicationRecord
  belongs_to :issue
  belongs_to :organization_user
  has_one :user, through: :organization_user
  has_one :organization, through: :issue

  has_rich_text :content
  validates :content, presence: true

  validate :organization_user_must_match_comment_organization

  private

  def organization_user_must_match_comment_organization
    return if organization_user.blank? || issue.blank?

    return if organization_user.organization_id == organization.id

    errors.add(:organization_user, "must belong to the same organization as the issue")
  end
end
