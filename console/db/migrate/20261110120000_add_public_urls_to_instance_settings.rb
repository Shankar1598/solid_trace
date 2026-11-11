# frozen_string_literal: true

# Console's URL and ingest's URL, as people, links and SDKs reach them, set on
# Settings → Instance (ADR 0017). Both start blank, and blank falls back to
# CONSOLE_URL and PUBLIC_INGEST_URL, then the request's address (PublicUrls),
# so the upgrade changes no install until an instance admin saves one.
class AddPublicUrlsToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :console_url, :string
    add_column :instance_settings, :ingest_url, :string
  end
end
