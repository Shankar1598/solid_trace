# frozen_string_literal: true

# A titled group of a Dashboard's Charts. The page shows the Charts with no
# Section first, then each Section in position order, its Charts on a grid of
# their own: a Chart's x and y are its place in its Section. When a Section is
# deleted, its Charts stay on the Dashboard with no Section, under the Charts
# there, in the same places to each other.
class DashboardSection < ApplicationRecord
  MAX_TITLE = 200

  belongs_to :dashboard, touch: true
  has_many :charts, class_name: "DashboardChart", foreign_key: :section_id, inverse_of: :section

  before_destroy :move_charts_to_no_section

  validates :title, presence: true, length: { maximum: MAX_TITLE }
  validate :room_on_the_dashboard, on: :create

  # Saves the Section at position, from 0, among its Dashboard's Sections, and
  # moves the others to make room. A position past the last puts it last. With
  # no position, a new Section goes last and a saved one keeps its place.
  def save_at!(position = nil)
    unless position.nil?
      index = Integer(position.to_s, 10, exception: false)
      return refuse_position("must be a whole number") if index.nil?
      return refuse_position("must be 0 or more") if index.negative?
    end
    return save! if index.nil? && persisted?

    transaction do
      others = dashboard.sections.where.not(id: id).to_a
      index = (index || others.size).clamp(0, others.size)
      self.position = index
      save!
      others.insert(index, self).each_with_index do |section, i|
        section.update_columns(position: i) unless section.position == i
      end
    end
    self
  end

  private

  def move_charts_to_no_section
    charts.update_all([ "section_id = NULL, y = y + ?", dashboard.bottom(nil) ])
  end

  def room_on_the_dashboard
    return unless dashboard && dashboard.sections.count >= Dashboard::MAX_SECTIONS

    errors.add(:dashboard, "has at most #{Dashboard::MAX_SECTIONS} Sections")
  end

  def refuse_position(message)
    errors.add(:position, message)
    raise ActiveRecord::RecordInvalid, self
  end
end
