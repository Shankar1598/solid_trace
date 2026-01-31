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

ActiveRecord::Schema[8.1].define(version: 2026_01_26_040000) do
  create_table "console_messages", force: :cascade do |t|
    t.integer "attempts", default: 0
    t.datetime "created_at", null: false
    t.text "error_message"
    t.string "message_type", null: false
    t.binary "payload", null: false
    t.datetime "processed_at"
    t.integer "status", default: 0, null: false
    t.datetime "updated_at", null: false
    t.index ["status", "created_at"], name: "index_console_messages_on_status_and_created_at"
  end

  create_table "event_store_messages", force: :cascade do |t|
    t.integer "attempts", default: 0
    t.datetime "created_at", null: false
    t.text "error_message"
    t.string "message_type", null: false
    t.binary "payload", null: false
    t.datetime "processed_at"
    t.integer "status", default: 0, null: false
    t.datetime "updated_at", null: false
    t.index ["status", "created_at"], name: "index_event_store_messages_on_status_and_created_at"
  end
end
