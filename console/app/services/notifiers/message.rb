# frozen_string_literal: true

module Notifiers
  # A message about a Monitor, in the words every provider adapter formats
  # its own way:
  # - title: one line, such as "Firing: Checkout errors"
  # - text: what happened, in a sentence or two
  # - fields: [label, value] pairs, such as ["Threshold", "> 100"]
  # - lines: Logs that matched, one line each, and how many more did
  # - link: the Logs explorer, showing what the Monitor saw
  # - baseline_link: comparing the change, the explorer over the Baseline
  #   range; nil otherwise
  # - severity: error (firing), warning (Logs matched) or info (resolved)
  # - dedup_key: the same for every message about one group of a Monitor,
  #   and for every Match message of one
  # - action: "trigger" or "resolve"
  #
  # It passes through a mail job as a Hash (to_h, then new(**hash)); a job
  # enqueued before baseline_link existed has none.
  Message = Data.define(:title, :text, :fields, :lines, :link, :severity, :dedup_key, :action, :baseline_link) do
    def initialize(baseline_link: nil, **rest)
      super(baseline_link: baseline_link, **rest)
    end

    def resolve?
      action == "resolve"
    end
  end
end
