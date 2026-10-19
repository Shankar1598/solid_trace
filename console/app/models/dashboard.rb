# frozen_string_literal: true

# A page of charts that belongs to an Organization. Each chart runs a saved
# Query on one of the Organization's Projects, over the range the page picks:
# default_range until a member picks another.
class Dashboard < ApplicationRecord
  AUTO_REFRESH = %w[off 15s 30s 1m 5m].freeze
  # A span back from now, as the explorer's links hold a range: 15m, 1h, 7d.
  SPAN = /\A0*[1-9]\d*[mhd]\z/

  belongs_to :organization
  belongs_to :created_by, class_name: "User", optional: true
  has_many :charts, -> { order(:y, :x, :id) }, class_name: "DashboardChart", inverse_of: :dashboard, dependent: :destroy

  validates :name, presence: true, length: { maximum: 200 }
  validates :description, length: { maximum: 2000 }
  validates :default_range, format: { with: SPAN, message: "must be a span such as 15m, 1h or 7d" }
  validates :auto_refresh, inclusion: { in: AUTO_REFRESH, message: "must be off, 15s, 30s, 1m or 5m" }

  # The row under every chart, where a new chart goes.
  def bottom
    charts.maximum(Arel.sql("y + h")) || 0
  end
end
