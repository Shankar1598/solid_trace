# Garnet - Error Tracking System

## Project Status

✅ **Full Stack Rails 8** - Running on http://localhost:3000
- **Frontend**: Hotwire (Turbo + Stimulus) + shadcn-rails + Tailwind CSS
- **Backend**: Rails 8 (API + Views)
- **Database**: PostgreSQL
- **Background Jobs**: Solid Queue
- **Caching**: Solid Cache

## Features

### Web Interface
- **Authentication**: Session-based login/signup.
- **Dashboard**: Overview of projects and stats.
- **Projects**: Create and manage projects.
- **Issues**: View list of issues and detailed event logs.

### Ingestion API (Sentry-compatible)
- `POST /api/:project_id/store/` - Accepts error events
- Authentication via `X-Sentry-Auth: Sentry sentry_key=<key>` header

## Running the Project

```bash
# Start the application (Rails server + Tailwind watcher)
cd garnet/backend
bin/dev
```

## Architecture

**Stack**: Pure Rails 8 with Hotwire
- **UI Components**: shadcn-rails (ViewComponents)
- **Styling**: Tailwind CSS (v4 via tailwindcss-rails)
- **Interactivity**: Stimulus.js
- **Assets**: Propshaft + Importmap

## Test Commands

```bash
# Send a test error
curl -X POST -H "Content-Type: application/json" \
  -H "X-Sentry-Auth: Sentry sentry_key=testkey123" \
  -d '{"message": "Test Error", "level": "error"}' \
  http://localhost:3000/api/1/store/
```
