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

ActiveRecord::Schema[8.1].define(version: 2026_10_22_120000) do
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
    t.index ["token_digest"], name: "index_api_tokens_on_token_digest", unique: true
    t.index ["user_id"], name: "index_api_tokens_on_user_id"
  end

  create_table "comments", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "issue_id", null: false
    t.integer "organization_user_id", null: false
    t.datetime "updated_at", null: false
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
    t.datetime "created_at", null: false
    t.integer "default_log_retention_days", default: 30, null: false
    t.boolean "log_all_reads", default: false, null: false
    t.integer "meta_organization_id"
    t.boolean "meta_organization_seeded", default: false, null: false
    t.boolean "sign_up_open", default: false, null: false
    t.boolean "sql_queries_enabled", default: true, null: false
    t.datetime "updated_at", null: false
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
    t.float "last_value"
    t.integer "monitor_id", null: false
    t.datetime "since", null: false
    t.string "status", default: "ok", null: false
    t.datetime "updated_at", null: false
    t.index ["monitor_id", "group_key"], name: "index_monitor_states_on_monitor_id_and_group_key", unique: true
  end

  create_table "monitors", force: :cascade do |t|
    t.boolean "alert_on_no_data", default: false, null: false
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

  create_table "organizations", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "name"
    t.string "slug"
    t.datetime "updated_at", null: false
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
    t.json "log_file_order"
    t.integer "log_retention_days"
    t.string "name"
    t.integer "organization_id", null: false
    t.string "slug"
    t.datetime "updated_at", null: false
    t.index ["organization_id"], name: "index_projects_on_organization_id"
    t.index ["slug"], name: "index_projects_on_slug"
  end

  create_table "sessions", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "ip_address"
    t.datetime "updated_at", null: false
    t.string "user_agent"
    t.integer "user_id", null: false
    t.index ["user_id"], name: "index_sessions_on_user_id"
  end

  create_table "users", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "email"
    t.boolean "invited", default: false, null: false
    t.string "name"
    t.string "password_digest"
    t.datetime "updated_at", null: false
    t.index ["email"], name: "index_users_on_email", unique: true
    t.index ["name"], name: "index_users_on_name"
  end

  add_foreign_key "active_storage_attachments", "active_storage_blobs", column: "blob_id"
  add_foreign_key "active_storage_variant_records", "active_storage_blobs", column: "blob_id"
  add_foreign_key "api_tokens", "users"
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
  add_foreign_key "organizations_users", "organizations"
  add_foreign_key "organizations_users", "users"
  add_foreign_key "project_issue_counters", "projects"
  add_foreign_key "project_keys", "projects"
  add_foreign_key "project_seen_cursors", "projects"
  add_foreign_key "projects", "organizations"
  add_foreign_key "sessions", "users"
end
