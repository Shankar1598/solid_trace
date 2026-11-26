# frozen_string_literal: true

module Notifiers
  # A message about a Monitor, in the words every provider adapter formats
  # its own way:
  # - title: one line, such as "Firing: Checkout errors"
  # - text: what happened, in a sentence or two
  # - fields: [label, value] pairs, such as ["Threshold", "> 100"]
  # - lines: Logs that matched, or Monitors, one line each
  # - more: how many more there are than lines holds
  # - link: the Logs explorer, showing what the Monitor saw, or the
  #   Monitors list for a Monitor status message
  # - link_text: the words of the link
  # - baseline_link: comparing the change, the explorer over the Baseline
  #   range; nil otherwise
  # - severity: error (firing), warning (Logs matched) or info (resolved)
  # - dedup_key: the same for every message about one group of a Monitor,
  #   and for every Match message of one
  # - action: "trigger" or "resolve", or nil for no PagerDuty event
  #
  # It passes through a mail job as a Hash (to_h, then new(**hash)); a job
  # enqueued before baseline_link, link_text or more existed has none.
  Message = Data.define(:title, :text, :fields, :lines, :link, :severity, :dedup_key, :action, :baseline_link,
    :link_text, :more) do
    def initialize(baseline_link: nil, link_text: "Open in the Logs explorer", more: 0, **rest)
      super(baseline_link: baseline_link, link_text: link_text, more: more, **rest)
    end

    def resolve?
      action == "resolve"
    end

    # lines, then "and N more" when there are more.
    def listed_lines
      more.positive? ? [ *lines, more_line(more) ] : lines
    end

    def more_line(count)
      "and #{count} more"
    end
  end
end
