# frozen_string_literal: true

require "securerandom"

class Event < ApplicationRecord
  self.primary_key = [ :issue_fingerprint_id, :created_at, :uuid ]

  before_validation :generate_uuid_v7, on: :create

  serialize :payload, coder: MessagePackCoder::Compressed

  belongs_to :issue_fingerprint
  belongs_to :project
  has_one :issue, through: :issue_fingerprint

  self.attributes_for_inspect = [ :uuid, :issue_fingerprint_id, :environment ]

  scope :lite, -> { select(column_names - [ :payload ]) }

  # Compute hash from event attributes for grouping
  def self.compute_hash(title:, culprit:, kind:, fingerprint: nil)
    # Build hash input based on fingerprint template
    if fingerprint.present?
      hash_input = fingerprint.map do |part|
        if part == "{{ default }}"
          # Expand default template
          "#{title}||#{culprit}||#{kind}"
        else
          part.to_s
        end
      end.join("||")
    else
      # No custom fingerprint, use default
      hash_input = "#{title}||#{culprit}||#{kind}"
    end

    Digest::MD5.hexdigest(hash_input)
  end

  private

  def generate_uuid_v7
    self.uuid ||= SecureRandom.uuid_v7
  end
end
