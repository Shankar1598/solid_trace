# frozen_string_literal: true

# For the controllers of a Dashboard's page. What the page shows rides in its
# query string: the range as range, or from and to, and each filter's values
# as v_<name>, the key repeated for several values:
# ?range=1h&v_service=api&v_service=web. A filter missing from it is All.
module DashboardView
  extend ActiveSupport::Concern

  private

  # The Dashboard's page, showing what the page this request came from
  # showed.
  def dashboard_page(dashboard)
    shown = params.permit(:range, :from, :to).to_h.merge(filter_keys)
    path = dashboard_path(dashboard, org_slug: @current_org.slug)
    shown.empty? ? path : "#{path}?#{Rack::Utils.build_query(shown)}"
  end

  # The query string's v_<name> keys, each with its list of values. Rails'
  # params keep only the last of a repeated key.
  def filter_keys
    Rack::Utils.parse_query(request.query_string).select { |key, _| key.start_with?("v_") }.transform_values { |values| Array(values) }
  end
end
