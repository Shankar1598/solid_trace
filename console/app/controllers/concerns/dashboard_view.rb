# frozen_string_literal: true

# For the controllers of a Dashboard's page. What the page shows rides in its
# query string: the range as range, or from and to, and each filter's values
# (Dashboard.filter_values_in).
module DashboardView
  extend ActiveSupport::Concern

  private

  # The Dashboard's page, showing what the page this request came from
  # showed.
  def dashboard_page(dashboard)
    shown = params.permit(:range, :from, :to).to_h.merge(Dashboard.filter_link_params(shown_filter_values))
    path = dashboard_path(dashboard, org_slug: @current_org.slug)
    shown.empty? ? path : "#{path}?#{Rack::Utils.build_query(shown)}"
  end

  # The filter values the page this request came from showed, {name =>
  # [values]}.
  def shown_filter_values
    Dashboard.filter_values_in(request.query_string)
  end
end
