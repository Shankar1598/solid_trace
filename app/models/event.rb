# frozen_string_literal: true

class Event < ApplicationRecord
  belongs_to :event_fingerprint
  has_one :event_payload, dependent: :destroy
  delegate :payload, to: :event_payload
  delegate :issue, to: :event_fingerprint

  self.attributes_for_inspect = [ :id, :event_fingerprint_id, :environment ]

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
end
