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

ActiveRecord::Schema[8.2].define(version: 2025_12_09_183000) do
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

  create_table "comments", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.integer "issue_id", null: false
    t.datetime "updated_at", null: false
    t.integer "user_id", null: false
    t.index ["issue_id"], name: "index_comments_on_issue_id"
    t.index ["user_id"], name: "index_comments_on_user_id"
  end

  create_table "events", primary_key: ["issue_fingerprint_id", "created_at", "uuid"], force: :cascade do |t|
    t.datetime "created_at", precision: nil, null: false
    t.text "environment", default: "unknown", null: false
    t.integer "issue_fingerprint_id", null: false
    t.binary "payload"
    t.integer "project_id", null: false
    t.binary "uuid", limit: 16, null: false
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
    t.datetime "created_at", null: false
    t.string "culprit"
    t.integer "kind", default: 0, null: false
    t.bigint "number", null: false
    t.integer "project_id", null: false
    t.integer "status", default: 0, null: false
    t.string "title", null: false
    t.datetime "updated_at", null: false
    t.index ["project_id", "number"], name: "index_issues_on_project_id_and_number", unique: true
    t.index ["project_id", "status"], name: "index_issues_on_project_id_and_status"
    t.index ["project_id"], name: "index_issues_on_project_id"
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
    t.integer "organization_id", null: false
    t.datetime "updated_at", null: false
    t.integer "user_id", null: false
    t.index ["organization_id", "user_id"], name: "index_organizations_users_on_organization_id_and_user_id", unique: true
    t.index ["organization_id"], name: "index_organizations_users_on_organization_id"
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

  create_table "projects", force: :cascade do |t|
    t.datetime "created_at", null: false
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
    t.string "name"
    t.string "password_digest"
    t.datetime "updated_at", null: false
    t.index ["email"], name: "index_users_on_email", unique: true
  end

  add_foreign_key "active_storage_attachments", "active_storage_blobs", column: "blob_id"
  add_foreign_key "active_storage_variant_records", "active_storage_blobs", column: "blob_id"
  add_foreign_key "comments", "issues"
  add_foreign_key "comments", "users"
  add_foreign_key "integrations", "organizations"
  add_foreign_key "issue_fingerprints", "issues"
  add_foreign_key "issue_fingerprints", "projects"
  add_foreign_key "issues", "projects"
  add_foreign_key "organizations_users", "organizations"
  add_foreign_key "organizations_users", "users"
  add_foreign_key "project_issue_counters", "projects"
  add_foreign_key "project_keys", "projects"
  add_foreign_key "projects", "organizations"
  add_foreign_key "sessions", "users"
end
