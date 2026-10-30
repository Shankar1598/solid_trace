# frozen_string_literal: true

module McpTools
  # An Organization's Projects, as its Project pickers list them, with each
  # one's volume over the last 24 hours.
  class ListProjects < Base
    description "Lists an Organization's Projects: the slug other tools take, the name, the description " \
                "of what the Project holds, to pick the Project to query, log_retention_days, how many days back its Logs go, " \
                "trace_retention_days, how many days back its traces' Spans go, and its volume: logs_per_hour and " \
                "bytes_per_hour (each Log's message and Attributes' JSON) over the last 24 hours, and files, its Services' files " \
                "with Logs on the whole UTC days those 24 hours touch (usually yesterday and today). " \
                "Use the volume to size a Query's range before running it: a search reads every byte of its range, " \
                "so on a busy Project search a few hours, or filter on service or file first. The volume is null when EventStore can't answer."
    read_only!
    takes_organization

    # The volume of a Project EventStore didn't answer for.
    NO_VOLUME = { logs_per_hour: 0, bytes_per_hour: 0, files: 0 }.freeze

    def run
      projects = organization.projects.order(:name).to_a
      listed = ProjectOptionSerializer.list(projects)
      volumes = volumes_of(projects)
      missing = volumes ? NO_VOLUME : NO_VOLUME.transform_values { nil }
      listed.each { |project| project.merge!(volumes&.dig(project[:id]) || missing) }

      answer = { organization: organization.slug, projects: listed }
      answer[:note] = "EventStore couldn't give the volumes, so they are null. Try again shortly." unless volumes
      json(answer, link: links.projects(organization))
    end

    private

    # Each Project's volume by its id, with its rates rounded, or nil when
    # EventStore can't answer: the Projects are still listed.
    def volumes_of(projects)
      return {} if projects.empty?

      EventStore.volumes!(project_ids: projects.map(&:id))["projects"].to_h do |volume|
        [ volume["project_id"], { logs_per_hour: rounded(volume["logs_per_hour"]),
                                  bytes_per_hour: rounded(volume["bytes_per_hour"]), files: volume["files"].to_i } ]
      end
    rescue EventStore::Unavailable, EventStore::Rejected => e
      Rails.logger.error("list_projects volumes: #{e.message}")
      nil
    end

    # A rate as a whole number, or to two places under 1, so a quiet
    # Project's few Logs don't round to none.
    def rounded(rate)
      rate = rate.to_f
      rate.positive? && rate < 1 ? rate.round(2) : rate.round
    end
  end
end
