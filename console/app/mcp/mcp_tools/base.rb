# frozen_string_literal: true

module McpTools
  # What every MCP server tool is built on. A tool is a subclass, named for
  # the tool (ListProjects is list_projects), that says what it does and
  # takes, and answers in #run as the user whose API token was sent:
  #
  #   class ListProjects < Base
  #     description "Lists an Organization's Projects."
  #     read_only!
  #     takes_organization
  #
  #     def run
  #       json({ projects: ... }, link: links.projects(organization))
  #     end
  #   end
  #
  # #run answers with #json or #text, ending with a Console link, or raises
  # ToolError saying what to do instead. Base answers the failures tools meet
  # (a record not found, a policy's refusal, EventStore's refusals, a busy
  # Console, a Logs answer to chart, a timeout) as tool errors the model can
  # act on.
  class Base < MCP::Tool
    ORGANIZATION = {
      type: "string",
      description: "The Organization's slug. Leave it out when you belong to one Organization; whoami lists yours.",
    }.freeze
    PROJECT = { type: "string", description: "The Project's slug, in the Organization. list_projects lists them." }.freeze
    RANGE = {
      type: "string",
      description: "How far back from now to read: a whole number of m, h or d, such as 15m, 1h, 24h or 7d. " \
                   "1h when neither range nor from and to is given.",
    }.freeze
    FROM = { type: "string", description: "Where to start reading, as an ISO 8601 time. Give it with to, instead of range." }.freeze
    TO = { type: "string", description: "Where to stop reading, as an ISO 8601 time. Give it with from, instead of range." }.freeze
    # How to write a Query that reads less, for the tools that run or save one.
    READING_LESS = "To read less, filter with == or in on the Project's log_file_order (list_projects): " \
                   "query_language_reference's Reading less says how.".freeze

    # A record's name and the tool that lists its kind, for when one isn't
    # found.
    LISTED_BY = {
      "Organization" => [ "Organization", "whoami" ],
      "Project" => [ "Project", "list_projects" ],
      "Dashboard" => [ "Dashboard", "list_dashboards" ],
      "DashboardChart" => [ "Chart", "get_dashboard" ],
      "LogMonitor" => [ "Monitor", "list_monitors" ],
      "Integration" => [ "Integration", "list_integrations" ],
      "Issue" => [ "Issue", "list_issues" ],
      "Event" => [ "Event", "get_issue" ],
      "ProjectKey" => [ "key", "get_project_setup" ],
      "OrganizationUser" => [ "member", "list_members" ],
      "LogTransformVersion" => [ "Log transform version", "get_log_transform" ],
    }.freeze

    class << self
      # The server calls a tool's class with its arguments, timed
      # (CallTimings).
      def call(server_context:, **arguments)
        user = server_context[:user]
        CallTimings.mcp_tool(name_value, arguments, user) do
          new(user: user, console_url: server_context[:console_url], arguments: arguments).answer
        end
      end

      # A tool that only reads.
      def read_only!
        annotations(read_only_hint: true, destructive_hint: false, idempotent_hint: true, open_world_hint: false)
      end

      # A tool that makes or changes things without destroying any.
      # open_world says it also reaches outside SolidTrace, as a Monitor's
      # run tells its Integrations.
      def writes!(open_world: false)
        annotations(read_only_hint: false, destructive_hint: false, idempotent_hint: false, open_world_hint: open_world)
      end

      # A tool that deletes or replaces things, such as a key it rotates.
      def destructive!(open_world: false)
        annotations(read_only_hint: false, destructive_hint: true, idempotent_hint: false, open_world_hint: open_world)
      end

      # Adds an argument, as a JSON Schema, to the tool's input schema. The
      # tool reads it as arguments[name].
      def argument(name, schema, required: false)
        @properties = properties.merge(name.to_sym => schema)
        @required = (required_arguments + (required ? [ name.to_sym ] : [])).uniq
        input_schema({ properties: @properties, required: @required.presence }.compact)
      end

      # The organization argument, which #organization reads.
      def takes_organization
        argument :organization, ORGANIZATION
      end

      # The organization and project arguments, which #project reads.
      def takes_project
        takes_organization
        argument :project, PROJECT, required: true
      end

      # The range, from and to arguments, which #time_range reads.
      def takes_range
        argument :range, RANGE
        argument :from, FROM
        argument :to, TO
      end

      # A tool listed only while SQL queries are on, such as run_sql.
      def needs_sql_queries!
        @needs_sql_queries = true
      end

      def needs_sql_queries?
        @needs_sql_queries || false
      end

      # Whether the server lists the tool now.
      def listed?
        !needs_sql_queries? || InstanceSetting.current.sql_queries_enabled?
      end

      private

      def properties
        @properties || {}
      end

      def required_arguments
        @required || []
      end
    end

    attr_reader :user, :console_url, :arguments

    def initialize(user:, console_url:, arguments: {})
      @user = user
      @console_url = console_url
      @arguments = arguments
    end

    # The tool's answer: what #run answers, or a tool error.
    def answer
      run
    rescue ToolError => e
      error(e.message)
    rescue ActiveRecord::RecordNotFound => e
      error(not_found(e))
    rescue ActiveRecord::RecordInvalid => e
      error("Not saved: #{e.record.errors.full_messages.join('; ')}.")
    rescue Pundit::NotAuthorizedError
      error("Only an Organization admin can do this.")
    rescue EventStore::Rejected => e
      error(EventStore.mistakes(e.body).join("\n"))
    rescue EventStore::SQLQueriesOff => e
      error(e.message)
    rescue ChartRuns::Busy
      error("Console is busy running other Queries and charts. Run this again in a moment.")
    rescue ChartRuns::NotATable => e
      error(e.message)
    rescue EventStore::TimedOut => e
      error(timed_out(e))
    rescue EventStore::Unavailable => e
      Rails.logger.error(e.message)
      error("EventStore is unavailable. Try again shortly.")
    end

    # What the tool does, answered with #json or #text.
    def run
      raise NotImplementedError, "#{self.class} must define run"
    end

    private

    # The Organization the organization argument names, among the user's.
    # It may be left out when the user belongs to exactly one.
    def organization
      @organization ||= begin
        slug = arguments[:organization].presence
        organizations = user.organizations.order(:slug).to_a
        slugs = organizations.map(&:slug).join(", ")

        if slug
          organizations.find { |org| org.slug == slug } ||
            raise(ToolError, "No Organization #{slug} among yours: #{slugs.presence || 'you belong to none'}.")
        elsif organizations.one?
          organizations.sole
        else
          raise ToolError, organizations.empty? ? "You don't belong to an Organization." : "Give organization, one of: #{slugs}."
        end
      end
    end

    # The Project the project argument names, in #organization.
    def project
      @project ||= organization.projects.find_by(slug: arguments[:project].to_s) ||
        raise(ToolError, "No Project #{arguments[:project]} in #{organization.slug}. list_projects lists them.")
    end

    # A Query's leading source, such as ['shop'], as the chart editor reads it.
    SOURCE = /\A\s*\[\s*'(?:[^'\\]|\\.)*'\s*\]/

    # The Query with its leading source, if it has one, naming the Project of
    # slug instead, as the chart editor and the Monitor form do when a Chart's
    # or a Monitor's Project changes. EventStore refuses a Query whose source
    # names another Project's Logs.
    def with_source(query, slug)
      query.sub(SOURCE) { "['#{slug.gsub('\\') { '\\\\' }.gsub("'") { "\\'" }}']" }
    end

    # The range, from and to arguments as a TimeRange.
    def time_range
      @time_range ||= TimeRange.parse(range: arguments[:range], from: arguments[:from], to: arguments[:to])
    end

    # Runs the user's policy for record, as the UI does, and raises a tool
    # error when it refuses. doing says what was refused, such as "rotate a
    # Project's keys".
    def authorize!(record, query, doing)
      Pundit.authorize(user, record, query)
    rescue Pundit::NotAuthorizedError
      raise ToolError, "Only an Organization admin can #{doing}."
    end

    def policy(record)
      Pundit.policy!(user, record)
    end

    def links
      @links ||= Links.new(console_url)
    end

    # Answers value as compact JSON, ending with its Console link.
    def json(value, link: nil)
      text(JSON.generate(value), link: link)
    end

    # Answers body, ending with its Console link.
    def text(body, link: nil)
      MCP::Tool::Response.new([ { type: "text", text: link ? "#{body}\nConsole: #{link}" : body } ])
    end

    def error(message)
      MCP::Tool::Response.new([ { type: "text", text: message } ], error: true)
    end

    def not_found(error)
      noun, lister = LISTED_BY[error.model.to_s]
      noun ? "No such #{noun}. #{lister} lists them." : "Not found."
    end

    # EventStore's own message, which says the timeout and what to try, or a
    # generic one.
    def timed_out(error)
      return "The run was stopped at its timeout. Try a shorter range or a narrower Query." unless error.body.is_a?(Hash)

      "#{EventStore.error_text(error.body).upcase_first}."
    end
  end
end
