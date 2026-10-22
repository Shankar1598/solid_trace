# frozen_string_literal: true

module McpTools
  # The Console pages a tool's answers link to, so a person can open what the
  # agent saw.
  class Links
    def initialize(console_url)
      @console_url = console_url.chomp("/")
    end

    def user_settings
      url(routes.user_settings_path)
    end

    def projects(organization)
      url(routes.projects_path(org_slug: organization.slug))
    end

    def project(project)
      url(routes.project_path(project, org_slug: project.organization.slug))
    end

    # A Project's Log transform page: its program, versions and running
    # counts.
    def log_transform(project)
      url(routes.project_log_transform_path(project, org_slug: project.organization.slug))
    end

    # The Logs explorer on a Project, with a Query or SQL and a TimeRange, as
    # lib/logs/explorerState.ts reads its URL. viz is how a table is drawn
    # (line, area, bars or table); the explorer picks when it's nil.
    def explorer(project, range:, query: nil, sql: nil, viz: nil)
      search = range.link_params.merge(mode: sql ? "sql" : "query", q: query.presence, sql: sql.presence, viz: viz).compact
      url(routes.project_logs_path(project, org_slug: project.organization.slug, **search))
    end

    # A fresh Invite link for an Invited user to organization.
    def invite(user, organization)
      user.invite_url(organization, console_url: @console_url)
    end

    # The Organization's Members page.
    def members(organization)
      url(routes.organization_users_path(org_slug: organization.slug))
    end

    def dashboards(organization)
      url(routes.dashboards_path(org_slug: organization.slug))
    end

    # A Dashboard, over its default range or the TimeRange given, with the
    # filter values given, {name => [values]}, as the page's v_<name> keys,
    # repeated for several values.
    def dashboard(dashboard, range: nil, values: {})
      search = (range ? range.link_params : {}).merge(values.transform_keys { |name| "v_#{name}" })
      path = routes.dashboard_path(dashboard, org_slug: dashboard.organization.slug)
      url(search.empty? ? path : "#{path}?#{Rack::Utils.build_query(search)}")
    end

    def monitor(monitor)
      url(routes.monitor_path(monitor, org_slug: monitor.organization.slug))
    end

    def monitors(organization)
      url(routes.monitors_path(org_slug: organization.slug))
    end

    def integrations(organization)
      url(routes.integrations_path(org_slug: organization.slug))
    end

    # The Issues page, with the filters given: status, query, project_id,
    # range, sort and page, as IssuesController#index takes them.
    def issues(organization, **filters)
      url(routes.issues_path(org_slug: organization.slug, **filters.compact))
    end

    # An Issue's page, on its latest Event or the Event whose id is event.
    def issue(issue, event: nil)
      url(routes.project_issue_path(issue.project, issue, org_slug: issue.project.organization.slug, event_id: event.presence))
    end

    private

    def routes
      Rails.application.routes.url_helpers
    end

    def url(path)
      "#{@console_url}#{path}"
    end
  end
end
