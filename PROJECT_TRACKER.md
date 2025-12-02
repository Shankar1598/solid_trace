# Garnet Project - Development Tracker

**Project Goal:** Create a light weight Sentry alternative for error tracking using Rails 8 API + shadcn/ui frontend.

**Project Name:** Garnet

**Start Date:** December 1, 2025

---

## Table of Contents
1. [Project Overview](#project-overview)
2. [Architecture Decisions](#architecture-decisions)
3. [Implementation Progress](#implementation-progress)
4. [Current Status](#current-status)
5. [Known Issues](#known-issues)
6. [Next Steps](#next-steps)

---

## Project Overview

### Objective
Create a modern error tracking system based on Sentry's  design, using:
- **Backend:** Rails 8 API with Solid Queue, Solid Cache, PostgreSQL
- **Frontend:** Vite + React + TypeScript + shadcn/ui
- **Focus:** Error tracking only (excluding performance monitoring, session replay and any other features.)

### Key Requirements
- Sentry SDK compatibility for ingestion
- Simple, maintainable codebase
- Dockerized development environment
- Minitest for backend testing

---

## Architecture Decisions

### Backend Stack
- **Framework:** Rails 8.1.1 (API mode)
- **Database:** PostgreSQL 18
- **Background Jobs:** Solid Queue (Rails 8 default)
- **Caching:** Solid Cache (Rails 8 default)
- **Testing:** Minitest (Rails default)
- **Deployment:** Docker Compose

### Frontend Stack
- **Build Tool:** Vite 7.2.6
- **Framework:** React 19.2.0 + TypeScript
- **UI Library:** shadcn/ui
- **Styling:** Tailwind CSS 3.4.17
- **Routing:** React Router
- **Data Fetching:** TanStack Query (React Query)
- **HTTP Client:** Axios

### Data Model
```
Organization
  └─ Project
       ├─ ProjectKey (for DSN authentication)
       └─ Issue (grouped errors)
            └─ IssueEvent (individual error occurrences)
Users
  └─ Organizations
```

---

## Implementation Progress

### ✅ Phase 1: Analysis & Planning (Completed)

### ✅ Phase 2: Backend Setup (Completed)

### ✅ Phase 3: Ingestion API (Completed)

### ✅ Phase 4: Management API (Completed)

### ✅ Phase 5: Frontend Setup (Completed)

### ✅ Phase 6: Frontend Components (Completed)

### ✅ Phase 7: Integration (Completed)

---

## Current Status

### Test Data Available
- Check seeds.rb file
- Add whatever test data you need in that file.

---

## Next Steps

### Short Term (Complete MVP)
1. **UI Polish**
   - [x] Proper froentend navigation for Issues, Settings, users, etc. with a left sidebar.
   - [x] UI and UX polish resembling Sentry and color scheme.
   - [x] Add loading states
   - [ ] Add error boundaries (Component exists but not integrated globally)
   - [x] Improve responsive design
   - [x] Add dark mode toggle
   - [x] What else is missing? I want this to be a greate Sentry alternative for error tracking.

2. **Enhanced Error Display**
   - [x] Parse stacktraces from event data
   - [x] Format exception details
   - [x] Display breadcrumbs if present
   - [x] Show request/user context

3. **Search & Filter**
   - Filter by level
   - Filter by status
   - Search by title
   - Date range filter

### Medium Term (Production Ready)
6. **Better Issue Grouping**
   - Implement fingerprinting algorithm
   - Use stacktrace for grouping
   - Support custom fingerprint field

7. **Issue Management**
   - Resolve/unresolve issues
   - Assign to users
   - Add comments
   - Set priority

8. **Testing**
   - Write Minitest tests for controllers
   - Add Vitest tests for frontend components
   - Integration tests

### Long Term (Feature Complete)
9. **User Management**
   - Authentication
   - Organizations/teams
   - Permissions

10. **Advanced Features**
    - Source maps support
    - Release tracking
    - User feedback
    - Email notifications
    - Webhook integrations

---

## Commands Reference

```bash
# Start services
docker compose up -d

# View logs
docker compose logs backend frontend -f
```

### Testing APIs
```bash
# Send test error
curl -X POST -H "Content-Type: application/json" \
  -H "X-Sentry-Auth: Sentry sentry_key=testkey123" \
  -d '{"message": "Test Error", "level": "error"}' \
  http://localhost:3000/api/1/store/

# List issues
curl http://localhost:3000/api/v1/projects/1/issues | jq

# Get issue details
curl http://localhost:3000/api/v1/issues/1 | jq
```

---

## Development Notes

### Design Decisions Made

1. **Simplified Issue Grouping:** Using title matching for MVP instead of complex fingerprinting
2. **JSONB for Event Data:** Flexible storage for varying event structures
3. **Minitest over RSpec:** User preference, Rails default
4. **Tailwind v3:** shadcn/ui compatibility (v4 caused initialization issues)
5. **Docker-First:** All development in containers for consistency
6. **No Authentication Yet:** Will add in later phase

---

## Success Metrics

- [x] Backend API running
- [x] Database models created
- [x] Error ingestion working
- [x] Management API working
- [x] Frontend UI built
- [x] Frontend-backend integration working
- [ ] End-to-end error flow tested
- [ ] Documentation complete

---
