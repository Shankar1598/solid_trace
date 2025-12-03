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

ActiveRecord::Schema[8.1].define(version: 2025_12_03_181541) do
  create_table "environments", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "name"
    t.integer "project_id", null: false
    t.datetime "updated_at", null: false
    t.index ["project_id", "name"], name: "index_environments_on_project_id_and_name", unique: true
    t.index ["project_id"], name: "index_environments_on_project_id"
  end

  create_table "issue_events", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.json "data"
    t.integer "issue_id", null: false
    t.datetime "updated_at", null: false
    t.index ["issue_id"], name: "index_issue_events_on_issue_id"
  end

  create_table "issue_fingerprints", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "fingerprint"
    t.integer "issue_id", null: false
    t.datetime "updated_at", null: false
    t.index ["issue_id", "fingerprint"], name: "index_issue_fingerprints_on_issue_id_and_fingerprint", unique: true
    t.index ["issue_id"], name: "index_issue_fingerprints_on_issue_id"
  end

  create_table "issues", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "culprit"
    t.string "event_type"
    t.integer "level"
    t.integer "project_id", null: false
    t.integer "status"
    t.string "title"
    t.datetime "updated_at", null: false
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

  add_foreign_key "environments", "projects"
  add_foreign_key "issue_events", "issues"
  add_foreign_key "issue_fingerprints", "issues"
  add_foreign_key "issues", "projects"
  add_foreign_key "organizations_users", "organizations"
  add_foreign_key "organizations_users", "users"
  add_foreign_key "project_keys", "projects"
  add_foreign_key "projects", "organizations"
  add_foreign_key "sessions", "users"
end
