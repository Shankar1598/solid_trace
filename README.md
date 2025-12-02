# Garnet - Error Tracking System

## Project Status

✅ **Backend (Rails 8 API)** - Running on http://localhost:3000
- Docker Compose setup with PostgreSQL
- Models: Organization, Project, ProjectKey, Issue, IssueEvent
- Ingestion API (Sentry-compatible)
- Management API for listing issues

## What's Working

### Backend APIs

1. **Ingestion API** (Sentry-compatible):
   - `POST /api/:project_id/store/` - Accepts error events
   - Authentication via `X-Sentry-Auth: Sentry sentry_key=<key>` header

2. **Management API**:
   - `GET /api/v1/projects/:project_id/issues` - List all issues for a project
   - `GET /api/v1/issues/:id` - Get issue details with events

### Test Data
- Organization: "Garnet Org" (ID: 1)
- Project: "Garnet Project" (ID: 1)
- Project Key: `testkey123`

### Test Commands

```bash
# Send a test error
curl -X POST -H "Content-Type: application/json" \
  -H "X-Sentry-Auth: Sentry sentry_key=testkey123" \
  -d '{"message": "Test Error", "level": "error"}' \
  http://localhost:3000/api/1/store/

# List issues
curl http://localhost:3000/api/v1/projects/1/issues | jq

# Get issue details
curl http://localhost:3000/api/v1/issues/1 | jq
```

## Next Steps

### Frontend (Vite + React + shadcn/ui)
- ✅ Initialized with Vite
- ✅ Installed shadcn/ui
- ⏳ Need to create:
  - Issue List component
  - Issue Detail component
  - API client
  - Routing

### Future Enhancements
- Better issue grouping (fingerprinting)
- Stacktrace parsing and display
- User feedback
- Source maps support
- Breadcrumbs
- Tags and metadata
- Search and filtering
- Issue resolution workflow

## Running the Project

```bash
# Start backend
cd garnet
docker compose up

# In another terminal, start frontend (when ready)
cd garnet/frontend
npm run dev
```

## Architecture

**Backend**: Rails 8 API mode with:
- Solid Queue (background jobs)
- Solid Cache (caching)
- PostgreSQL (database)
- CORS enabled for frontend

**Frontend**: Vite + React + TypeScript with:
- shadcn/ui components
- Tailwind CSS
- React Router (to be added)
- TanStack Query (to be added)
