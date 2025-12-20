# Migrate Garnet from Hotwire/Turbo to Inertia + React

## Backend Tasks

### BE1: Foundation & Configuration
- [x] Add `vite_rails` and `inertia_rails` gems
- [x] Remove `importmap-rails`, `turbo-rails`, `stimulus-rails` gems
- [x] Configure Inertia shared data (current_user, flash, etc.)
- [x] Create serializers for models (Issue, Event, Project, etc.)

### BE2: Controller Conversions
- [x] Convert `SessionsController` to Inertia responses
- [x] Convert `RegistrationsController` to Inertia responses
- [x] Convert `IssuesController` to Inertia responses
- [x] Convert `ProjectsController` to Inertia responses
- [x] Convert `CommentsController` (replace Turbo Stream with Inertia redirect)
- [x] Convert `IntegrationsController` to Inertia responses
- [x] Convert `OrganizationSettingsController` to Inertia responses
- [x] Convert `OrganizationMembersController` to Inertia responses
- [x] Convert `UserSettingsController` to Inertia responses

---

## Frontend Tasks

### FE1: Foundation & Setup
- [x] Set up Vite + React + TypeScript
- [x] Configure ShadCN/UI and install base components
- [x] Create TypeScript type definitions for models
- [x] Create utility functions (`cn()`, etc.)

### FE2: Layout & Auth Components
- [x] Create `DashboardLayout` component (sidebar, header, theme toggle)
- [x] Create `AuthLayout` component
- [x] Create `Sessions/New.tsx` (login page)
- [x] Create `Registrations/New.tsx` (registration page)

### FE3: Issues Pages & Components
- [x] Create `Issues/Index.tsx` (list with filters)
- [x] Create `Issues/Show.tsx` (detail view with tabs)
- [x] Create `Stacktrace.tsx` component
- [x] Create `Breadcrumbs.tsx` component
- [x] Create `CommentList.tsx` and `CommentForm.tsx` (merged into one)
- [x] Create `RichTextEditor.tsx` component (replacing Lexxy with Tiptap) for rich text comments

### FE4: Projects, Settings & Integrations
- [x] Create `Projects/Index.tsx` and `Projects/Show.tsx`
- [x] Create `Projects/New.tsx`
- [x] Create `Settings/Organization.tsx`
- [x] Create `Settings/User.tsx`
- [x] Create `Integrations/Index.tsx`
- [x] Create `Integrations/New.tsx` and `Integrations/Edit.tsx`
- [x] Create `OrganizationMembers` component (merged into `Settings/Organization`)

---

## Cleanup
- [x] Delete unused ERB templates
- [x] Delete Stimulus controllers
- [x] Delete `config/importmap.rb`
- [ ] Update/fix tests for Inertia responses
