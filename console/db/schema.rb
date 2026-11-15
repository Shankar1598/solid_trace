# This file is auto-generated from the current state of the database. Instead
# of editing this file, please use the migrations feature of Active Record to
# incrementally modify your database, and then regenerate this schema definition.
#
# This file is the source Rails uses to define your schema when running `bin/rails
# db:schema:load`. When creating a new database, `bin/rails db:schema:load` tends to
# be faster and is potentially less error prone than running all of your
# migrations from scratch. Old migrations may fail to apply correctly if those
# migrations use external dependencies or application code.
#
# It's strongly recommended that you check this file into your version control system.

ActiveRecord::Schema[8.1].define(version: 2026_11_15_150000) do
  create_table "action_text_rich_texts", force: :cascade do |t|
    t.text "body"
    t.datetime "created_at", null: false
    t.string "name", null: false
    t.bigint "record_id", null: false
    t.string "record_type", null: false
    t.datetime "updated_at", null: false
    t.index ["record_type", "record_id", "name"], name: "index_action_text_rich_texts_uniqueness", unique: true
  end

  create_table "active_storage_attachments", force: :cascade do |t|
    t.bigint "blob_id", null: false
    t.datetime "created_at", null: false
    t.string "name", null: false
    t.bigint "record_id", null: false
    t.string "record_type", null: false
    t.index ["blob_id"], name: "index_active_storage_attachments_on_blob_id"
    t.index ["record_type", "record_id", "name", "blob_id"], name: "index_active_storage_attachments_uniqueness", unique: true
  end

  create_table "active_storage_blobs", force: :cascade do |t|
    t.bigint "byte_size", null: false
    t.string "checksum"
    t.string "content_type"
    t.datetime "created_at", null: false
    t.string "filename", null: false
    t.string "key", null: false
    t.text "metadata"
    t.string "service_name", null: false
    t.index ["key"], name: "index_active_storage_blobs_on_key", unique: true
  end

  create_table "active_storage_variant_records", force: :cascade do |t|
    t.bigint "blob_id", null: false
    t.string "variation_digest", null: false
    t.index ["blob_id", "variation_digest"], name: "index_active_storage_variant_records_uniqueness", unique: true
  end

  create_table "api_tokens", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.datetime "last_used_at"
    t.string "name", null: false
    t.string "token_digest", null: false
    t.datetime "updated_at", null: false
    t.integer "user_id", null: false
    t.json "scopes", default: [], null: false
    t.integer "organization_id"
    t.datetime "expires_at"
    t.json "agent_session_ids"
    t.integer "project_id"
    t.integer "parent_id"
    t.index ["organization_id"], name: "index_api_tokens_on_organization_id"
    t.index ["parent_id"], name: "index_api_tokens_on_parent_id"
    t.index ["project_id"], name: "index_api_tokens_on_project_id"
    t.index ["token_digest"], name: "index_api_tokens_on_token_digest", unique: true
    t.index ["user_id"], name: "index_api_tokens_on_user_id"
  end

  create_table "capture_confirmations", force: :cascade do |t|
    t.integer "project_id", null: false
    t.integer "user_id"
    t.json "entries", default: [], null: false
    t.datetime "created_at", null: false
    t.index ["project_id"], name: "index_capture_confirmations_on_project_id"
    t.index ["user_id"], name: "index_capture_confirmations_on_user_id"
  end

  create_table "comments", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "issue_id", null: false
    t.integer "organization_user_id"
    t.datetime "updated_at", null: false
    t.string "run_id"
    t.index ["issue_id"], name: "index_comments_on_issue_id"
    t.index ["organization_user_id"], name: "index_comments_on_organization_user_id"
  end

  create_table "dashboard_charts", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "dashboard_id", null: false
    t.integer "h", default: 4, null: false
    t.json "options", default: {}, null: false
    t.integer "project_id", null: false
    t.text "query", null: false
    t.string "title", null: false
    t.datetime "updated_at", null: false
    t.string "viz", default: "line", null: false
    t.integer "w", default: 6, null: false
    t.integer "x", default: 0, null: false
    t.integer "y", default: 0, null: false
    t.index ["dashboard_id"], name: "index_dashboard_charts_on_dashboard_id"
    t.index ["project_id"], name: "index_dashboard_charts_on_project_id"
  end

  create_table "dashboards", force: :cascade do |t|
    t.string "auto_refresh", default: "15s", null: false
    t.datetime "created_at", null: false
    t.integer "created_by_id"
    t.string "default_range", default: "1h", null: false
    t.text "description"
    t.json "filters", default: [], null: false
    t.string "name", null: false
    t.integer "organization_id", null: false
    t.datetime "updated_at", null: false
    t.index ["created_by_id"], name: "index_dashboards_on_created_by_id"
    t.index ["organization_id"], name: "index_dashboards_on_organization_id"
  end

  create_table "instance_settings", force: :cascade do |t|
    t.string "console_url"
    t.datetime "created_at", null: false
    t.integer "default_log_retention_days", default: 30, null: false
    t.integer "default_trace_retention_days", default: 14, null: false
    t.string "ingest_url"
    t.integer "local_cache_hours", default: 24, null: false
    t.boolean "log_all_reads", default: false, null: false
    t.boolean "log_mcp_reads", default: true, null: false
    t.integer "logs_read_threads", default: 0, null: false
    t.integer "logs_text_match_budget_mb", default: 4096, null: false
    t.string "mail_from"
    t.integer "meta_organization_id"
    t.boolean "meta_organization_seeded", default: false, null: false
    t.json "redaction_patterns", default: [], null: false
    t.boolean "send_console_issues", default: true, null: false
    t.boolean "sign_up_open", default: false, null: false
    t.string "smtp_address"
    t.text "smtp_password"
    t.integer "smtp_port", default: 587, null: false
    t.string "smtp_username"
    t.boolean "sql_queries_enabled", default: true, null: false
    t.boolean "trace_console", default: true, null: false
    t.datetime "updated_at", null: false
    t.string "dispatcher_url"
    t.index ["meta_organization_id"], name: "index_instance_settings_on_meta_organization_id"
  end

  create_table "integrations", force: :cascade do |t|
    t.boolean "active", default: true
    t.datetime "created_at", null: false
    t.string "name"
    t.integer "organization_id", null: false
    t.string "provider", null: false
    t.json "settings", default: {}
    t.datetime "updated_at", null: false
    t.index ["organization_id", "provider"], name: "index_integrations_on_organization_id_and_provider"
    t.index ["organization_id"], name: "index_integrations_on_organization_id"
  end

  create_table "issue_fingerprints", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "fingerprint"
    t.integer "issue_id", null: false
    t.integer "project_id", null: false
    t.datetime "updated_at", null: false
    t.index ["issue_id"], name: "index_issue_fingerprints_on_issue_id"
    t.index ["project_id", "fingerprint"], name: "index_issue_fingerprints_on_project_id_and_fingerprint", unique: true
  end

  create_table "issues", force: :cascade do |t|
    t.integer "assignee_id"
    t.datetime "created_at", null: false
    t.string "culprit"
    t.datetime "first_seen_at"
    t.integer "kind", default: 0, null: false
    t.datetime "last_seen_at"
    t.bigint "number", null: false
    t.integer "project_id", null: false
    t.integer "status", default: 0, null: false
    t.bigint "times_seen", default: 0, null: false
    t.string "title", null: false
    t.datetime "updated_at", null: false
    t.index ["project_id", "number"], name: "index_issues_on_project_id_and_number", unique: true
    t.index ["project_id", "status"], name: "index_issues_on_project_id_and_status"
  end

  create_table "log_transform_versions", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.text "program", default: "", null: false
    t.integer "project_id", null: false
    t.integer "user_id"
    t.index ["project_id", "id"], name: "index_log_transform_versions_on_project_id_and_id"
    t.index ["user_id"], name: "index_log_transform_versions_on_user_id"
  end

  create_table "monitor_integrations", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "integration_id", null: false
    t.integer "monitor_id", null: false
    t.datetime "updated_at", null: false
    t.index ["integration_id"], name: "index_monitor_integrations_on_integration_id"
    t.index ["monitor_id", "integration_id"], name: "index_monitor_integrations_on_monitor_id_and_integration_id", unique: true
  end

  create_table "monitor_runs", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "elapsed_ms"
    t.text "error"
    t.integer "firing"
    t.boolean "logs_skipped", default: false, null: false
    t.integer "matched"
    t.integer "monitor_id", null: false
    t.datetime "ran_at", null: false
    t.string "status", null: false
    t.index ["monitor_id", "ran_at"], name: "index_monitor_runs_on_monitor_id_and_ran_at"
  end

  create_table "monitor_states", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "group_key", default: "", null: false
    t.float "last_baseline"
    t.float "last_value"
    t.integer "monitor_id", null: false
    t.datetime "since", null: false
    t.string "status", default: "ok", null: false
    t.datetime "updated_at", null: false
    t.index ["monitor_id", "group_key"], name: "index_monitor_states_on_monitor_id_and_group_key", unique: true
  end

  create_table "monitors", force: :cascade do |t|
    t.boolean "alert_on_no_data", default: false, null: false
    t.string "baseline"
    t.string "compare", default: "value", null: false
    t.datetime "created_at", null: false
    t.datetime "disabled_at"
    t.string "ingest_cursor"
    t.datetime "ingest_cursor_at"
    t.integer "interval_minutes", default: 1, null: false
    t.string "kind", null: false
    t.text "last_error"
    t.datetime "last_run_at"
    t.datetime "logs_skipped_at"
    t.string "name", null: false
    t.datetime "next_run_at"
    t.boolean "notify_by_group", default: false, null: false
    t.string "operator"
    t.integer "organization_id", null: false
    t.integer "project_id", null: false
    t.text "query", null: false
    t.integer "range_minutes", default: 5, null: false
    t.datetime "run_started_at"
    t.float "threshold"
    t.integer "timeouts_in_a_row", default: 0, null: false
    t.datetime "updated_at", null: false
    t.float "minimum"
    t.index ["organization_id"], name: "index_monitors_on_organization_id"
    t.index ["project_id"], name: "index_monitors_on_project_id"
  end

  create_table "notifications", force: :cascade do |t|
    t.integer "attempts", default: 0, null: false
    t.datetime "created_at", null: false
    t.text "error_message"
    t.string "group_key"
    t.integer "integration_id", null: false
    t.integer "issue_id"
    t.string "kind", null: false
    t.integer "monitor_id"
    t.binary "payload"
    t.datetime "processing_at"
    t.datetime "sent_at"
    t.integer "status", default: 0, null: false
    t.datetime "updated_at", null: false
    t.index ["integration_id", "issue_id", "kind", "created_at"], name: "index_notifications_on_integration_issue_kind_created_at"
    t.index ["integration_id", "kind", "status"], name: "index_notifications_on_integration_id_and_kind_and_status"
    t.index ["integration_id"], name: "index_notifications_on_integration_id"
    t.index ["monitor_id", "group_key", "created_at"], name: "index_notifications_on_monitor_group_created_at"
    t.index ["status", "created_at"], name: "index_notifications_on_status_and_created_at"
    t.check_constraint "(issue_id IS NOT NULL AND monitor_id IS NULL AND group_key IS NULL) OR (issue_id IS NULL AND monitor_id IS NOT NULL AND group_key IS NOT NULL)", name: "notifications_one_subject"
  end

  create_table "oauth_clients", force: :cascade do |t|
    t.string "kind", null: false
    t.string "client_id", null: false
    t.string "name", null: false
    t.json "redirect_uris", default: [], null: false
    t.json "grant_types", default: [], null: false
    t.string "token_endpoint_auth_method", default: "none", null: false
    t.string "client_secret_digest"
    t.json "metadata", default: {}, null: false
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.datetime "fetched_at"
    t.datetime "fresh_until"
    t.datetime "fetch_started_at"
    t.index ["client_id"], name: "index_oauth_clients_on_client_id", unique: true
  end

  create_table "oauth_codes", force: :cascade do |t|
    t.string "kind", null: false
    t.string "code_digest", null: false
    t.integer "oauth_client_id"
    t.integer "oauth_grant_id"
    t.string "code_challenge"
    t.string "redirect_uri"
    t.json "scopes", default: [], null: false
    t.string "resource"
    t.datetime "expires_at", null: false
    t.datetime "used_at"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.string "client_id"
    t.string "user_code_digest"
    t.string "ip_address"
    t.datetime "denied_at"
    t.datetime "last_poll_at"
    t.integer "poll_interval"
    t.index ["code_digest"], name: "index_oauth_codes_on_code_digest", unique: true
    t.index ["oauth_client_id"], name: "index_oauth_codes_on_oauth_client_id"
    t.index ["oauth_grant_id"], name: "index_oauth_codes_on_oauth_grant_id"
    t.index ["user_code_digest"], name: "index_oauth_codes_on_user_code_digest"
  end

  create_table "oauth_grants", force: :cascade do |t|
    t.integer "user_id", null: false
    t.integer "oauth_client_id", null: false
    t.integer "organization_id", null: false
    t.json "scopes", default: [], null: false
    t.json "resources", default: [], null: false
    t.string "approved_via", null: false
    t.string "user_agent"
    t.string "ip_address"
    t.datetime "approved_at", null: false
    t.datetime "first_token_at"
    t.datetime "last_used_at"
    t.datetime "revoked_at"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.index ["oauth_client_id"], name: "index_oauth_grants_on_oauth_client_id"
    t.index ["organization_id"], name: "index_oauth_grants_on_organization_id"
    t.index ["user_id"], name: "index_oauth_grants_on_user_id"
  end

  create_table "oauth_pending_requests", force: :cascade do |t|
    t.string "key", null: false
    t.integer "oauth_client_id"
    t.string "redirect_uri"
    t.string "response_type"
    t.string "code_challenge"
    t.string "code_challenge_method"
    t.string "scope"
    t.string "resource"
    t.text "state"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.string "client_id"
    t.integer "oauth_code_id"
    t.index ["key"], name: "index_oauth_pending_requests_on_key", unique: true
    t.index ["oauth_client_id"], name: "index_oauth_pending_requests_on_oauth_client_id"
    t.index ["oauth_code_id"], name: "index_oauth_pending_requests_on_oauth_code_id"
  end

  create_table "oauth_tokens", force: :cascade do |t|
    t.string "kind", null: false
    t.string "token_digest", null: false
    t.integer "oauth_grant_id", null: false
    t.string "resource"
    t.datetime "expires_at", null: false
    t.datetime "used_at"
    t.datetime "revoked_at"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.integer "replaced_by_id"
    t.text "kept_access_token"
    t.text "kept_refresh_token"
    t.index ["oauth_grant_id"], name: "index_oauth_tokens_on_oauth_grant_id"
    t.index ["replaced_by_id"], name: "index_oauth_tokens_on_replaced_by_id"
    t.index ["token_digest"], name: "index_oauth_tokens_on_token_digest", unique: true
  end

  create_table "organizations", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "name"
    t.string "slug"
    t.datetime "updated_at", null: false
    t.boolean "require_two_factor", default: false, null: false
    t.index ["slug"], name: "index_organizations_on_slug"
  end

  create_table "organizations_users", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.datetime "discarded_at"
    t.integer "organization_id", null: false
    t.integer "role", default: 0, null: false
    t.datetime "updated_at", null: false
    t.integer "user_id", null: false
    t.index ["organization_id", "discarded_at"], name: "index_organizations_users_on_organization_id_and_discarded_at"
    t.index ["organization_id", "user_id"], name: "index_organizations_users_on_organization_id_and_user_id", unique: true
    t.index ["user_id"], name: "index_organizations_users_on_user_id"
  end

  create_table "passkeys", force: :cascade do |t|
    t.integer "user_id", null: false
    t.string "credential_id", null: false
    t.string "public_key", null: false
    t.bigint "sign_count", default: 0, null: false
    t.string "name", null: false
    t.string "host", null: false
    t.datetime "last_used_at"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.index ["credential_id"], name: "index_passkeys_on_credential_id", unique: true
    t.index ["user_id"], name: "index_passkeys_on_user_id"
  end

  create_table "project_issue_counters", primary_key: "project_id", force: :cascade do |t|
    t.bigint "value", default: 0, null: false
    t.index ["project_id"], name: "index_project_issue_counters_on_project_id"
  end

  create_table "project_keys", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "project_id", null: false
    t.string "public_key"
    t.string "secret_key"
    t.datetime "updated_at", null: false
    t.index ["project_id"], name: "index_project_keys_on_project_id"
    t.index ["public_key"], name: "index_project_keys_on_public_key"
  end

  create_table "project_seen_cursors", primary_key: "project_id", force: :cascade do |t|
    t.string "event_uuid", null: false
    t.index ["project_id"], name: "index_project_seen_cursors_on_project_id"
  end

  create_table "projects", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.text "description"
    t.json "log_file_order"
    t.integer "log_retention_days"
    t.string "name"
    t.integer "organization_id", null: false
    t.string "slug"
    t.integer "trace_retention_days"
    t.datetime "updated_at", null: false
    t.boolean "agent", default: false, null: false
    t.json "captured_repositories", default: [], null: false
    t.index ["organization_id"], name: "index_projects_on_organization_id"
    t.index ["organization_id"], name: "index_projects_one_agent_project", unique: true, where: "agent"
    t.index ["slug"], name: "index_projects_on_slug"
  end

  create_table "recovery_codes", force: :cascade do |t|
    t.integer "user_id", null: false
    t.string "digest", null: false
    t.datetime "used_at"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.index ["user_id"], name: "index_recovery_codes_on_user_id"
  end

  create_table "sessions", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "ip_address"
    t.datetime "updated_at", null: false
    t.string "user_agent"
    t.integer "user_id", null: false
    t.datetime "expires_at", null: false
    t.index ["user_id"], name: "index_sessions_on_user_id"
  end

  create_table "users", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "email"
    t.boolean "instance_admin", default: false, null: false
    t.boolean "invited", default: false, null: false
    t.string "name"
    t.string "password_digest"
    t.datetime "updated_at", null: false
    t.string "authenticator_secret"
    t.datetime "authenticator_confirmed_at"
    t.integer "authenticator_last_step"
    t.string "webauthn_id"
    t.index ["email"], name: "index_users_on_email", unique: true
    t.index ["name"], name: "index_users_on_name"
    t.index ["webauthn_id"], name: "index_users_on_webauthn_id", unique: true
  end

  add_foreign_key "active_storage_attachments", "active_storage_blobs", column: "blob_id"
  add_foreign_key "active_storage_variant_records", "active_storage_blobs", column: "blob_id"
  add_foreign_key "api_tokens", "api_tokens", column: "parent_id", on_delete: :cascade
  add_foreign_key "api_tokens", "organizations", on_delete: :cascade
  add_foreign_key "api_tokens", "projects", on_delete: :cascade
  add_foreign_key "api_tokens", "users"
  add_foreign_key "capture_confirmations", "projects", on_delete: :cascade
  add_foreign_key "capture_confirmations", "users", on_delete: :nullify
  add_foreign_key "comments", "issues"
  add_foreign_key "comments", "organizations_users", column: "organization_user_id"
  add_foreign_key "dashboard_charts", "dashboards"
  add_foreign_key "dashboard_charts", "projects"
  add_foreign_key "dashboards", "organizations"
  add_foreign_key "dashboards", "users", column: "created_by_id", on_delete: :nullify
  add_foreign_key "instance_settings", "organizations", column: "meta_organization_id", on_delete: :nullify
  add_foreign_key "integrations", "organizations"
  add_foreign_key "issue_fingerprints", "issues"
  add_foreign_key "issue_fingerprints", "projects"
  add_foreign_key "issues", "organizations_users", column: "assignee_id"
  add_foreign_key "issues", "projects"
  add_foreign_key "log_transform_versions", "projects", on_delete: :cascade
  add_foreign_key "log_transform_versions", "users", on_delete: :nullify
  add_foreign_key "monitor_integrations", "integrations", on_delete: :cascade
  add_foreign_key "monitor_integrations", "monitors", on_delete: :cascade
  add_foreign_key "monitor_runs", "monitors", on_delete: :cascade
  add_foreign_key "monitor_states", "monitors", on_delete: :cascade
  add_foreign_key "monitors", "organizations"
  add_foreign_key "monitors", "projects"
  add_foreign_key "notifications", "integrations"
  add_foreign_key "oauth_codes", "oauth_clients"
  add_foreign_key "oauth_codes", "oauth_grants"
  add_foreign_key "oauth_grants", "oauth_clients"
  add_foreign_key "oauth_grants", "organizations"
  add_foreign_key "oauth_grants", "users"
  add_foreign_key "oauth_pending_requests", "oauth_clients"
  add_foreign_key "oauth_tokens", "oauth_grants"
  add_foreign_key "oauth_tokens", "oauth_tokens", column: "replaced_by_id", on_delete: :nullify
  add_foreign_key "organizations_users", "organizations"
  add_foreign_key "organizations_users", "users"
  add_foreign_key "passkeys", "users"
  add_foreign_key "project_issue_counters", "projects"
  add_foreign_key "project_keys", "projects"
  add_foreign_key "project_seen_cursors", "projects"
  add_foreign_key "projects", "organizations"
  add_foreign_key "recovery_codes", "users"
  add_foreign_key "sessions", "users"
end
