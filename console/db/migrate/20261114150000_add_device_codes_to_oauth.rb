# frozen_string_literal: true

# The device flow (RFC 8628). A device code is an oauth_codes row of kind
# device: the digest of its user code, the IP address that asked for it (the
# time is created_at), when it was denied, its last poll and its poll interval. A
# device code keeps only the client_id (the URL of the published
# description) until a signed-in user types the user code, because Console
# fetches nothing before that. A pending request on the device page points
# to its device code and has no redirect URI.
class AddDeviceCodesToOauth < ActiveRecord::Migration[8.1]
  def change
    add_column :oauth_codes, :client_id, :string
    add_column :oauth_codes, :user_code_digest, :string
    add_column :oauth_codes, :ip_address, :string
    add_column :oauth_codes, :denied_at, :datetime
    add_column :oauth_codes, :last_poll_at, :datetime
    add_column :oauth_codes, :poll_interval, :integer
    add_index :oauth_codes, :user_code_digest
    change_column_null :oauth_codes, :oauth_client_id, true

    add_reference :oauth_pending_requests, :oauth_code, foreign_key: false, index: true
    change_column_null :oauth_pending_requests, :redirect_uri, true
  end
end
