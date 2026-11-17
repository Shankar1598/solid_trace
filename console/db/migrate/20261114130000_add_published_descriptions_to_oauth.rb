# frozen_string_literal: true

# OAuth clients with a published description (a Client ID Metadata
# Document): the client keeps the time of its last fetch, the time until
# which that copy is fresh, and the start time of a fetch that runs now, so
# one fetch at a time runs for one client_id. A pending request for such a
# client keeps only its client_id (the URL) until the user signs in, because
# Console fetches nothing before sign-in.
class AddPublishedDescriptionsToOauth < ActiveRecord::Migration[8.1]
  def change
    add_column :oauth_clients, :fetched_at, :datetime
    add_column :oauth_clients, :fresh_until, :datetime
    add_column :oauth_clients, :fetch_started_at, :datetime

    add_column :oauth_pending_requests, :client_id, :string
    change_column_null :oauth_pending_requests, :oauth_client_id, true
  end
end
