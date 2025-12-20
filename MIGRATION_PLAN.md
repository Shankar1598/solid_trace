# Migrate Garnet from Hotwire/Turbo to Inertia.js + React

## Overview

This plan converts Garnet from Rails + Hotwire to **Inertia.js + React** with **Vite**, **TypeScript**, and **ShadCN/UI**.

### Breaking Changes
- Turbo Stream responses → Inertia partial reloads
- Stimulus controllers → React state/hooks
- Flowbite → ShadCN/UI
- Lexxy (ActionText) → TipTap

---

## BE1: Foundation & Configuration

### Gemfile Changes
```diff
- gem "importmap-rails"
- gem "turbo-rails"
- gem "stimulus-rails"
+ gem "vite_rails"
+ gem "inertia_rails"
```

### New Files
- `config/initializers/inertia.rb` - Inertia configuration
- `app/controllers/concerns/inertia_share.rb` - Shared props (current_user, flash)
- `app/serializers/*.rb` - JSON serializers for Issue, Event, Project, etc.

---

## BE2: Controller Conversions

Change all controllers from ERB to Inertia responses:

```ruby
# Example: IssuesController
def index
  render inertia: 'Issues/Index', props: {
    issues: @issues.map { |i| IssueSerializer.new(i) },
    environments: @environments,
    filters: { status: params[:status], query: params[:query] }
  }
end
```

**Controllers to convert:** Sessions, Registrations, Issues, Projects, Comments, Integrations, OrganizationSettings, OrganizationMembers, UserSettings

---

## FE1: Foundation & Setup

### Package.json
```json
{
  "devDependencies": {
    "vite": "^6.0.0",
    "vite-plugin-ruby": "^5.1.0",
    "@vitejs/plugin-react": "^4.3.0",
    "typescript": "^5.7.0"
  },
  "dependencies": {
    "@inertiajs/react": "^2.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "class-variance-authority": "^0.7.0",
    "clsx": "^2.1.0",
    "tailwind-merge": "^2.5.0",
    "lucide-react": "^0.468.0",
    "@tiptap/react": "^2.0.0"
  }
}
```

### New Files
- `vite.config.ts`
- `tsconfig.json`
- `app/frontend/entrypoints/application.tsx`
- `app/frontend/lib/utils.ts`
- `app/frontend/components/ui/*.tsx` (ShadCN)

---

## FE2: Layout & Auth

### Components
- `app/frontend/components/layouts/DashboardLayout.tsx`
- `app/frontend/components/Sidebar.tsx`

### Pages
- `app/frontend/pages/Sessions/New.tsx`
- `app/frontend/pages/Registrations/New.tsx`

---

## FE3: Issues

### Pages
- `app/frontend/pages/Issues/Index.tsx` - List with filters
- `app/frontend/pages/Issues/Show.tsx` - Detail with tabs

### Components
- `app/frontend/components/issues/Stacktrace.tsx`
- `app/frontend/components/issues/Breadcrumbs.tsx`
- `app/frontend/components/issues/CommentList.tsx`
- `app/frontend/components/issues/CommentForm.tsx`
- `app/frontend/components/issues/RichTextEditor.tsx` (TipTap)

---

## FE4: Projects, Settings & Integrations

### Pages
- `app/frontend/pages/Projects/{Index,Show}.tsx`
- `app/frontend/pages/Settings/{Organization,User}.tsx`
- `app/frontend/pages/Integrations/{Index,New,Edit}.tsx`

---

## File Structure After Migration

```
app/
├── controllers/           # Inertia responses
├── serializers/           # JSON serializers
├── frontend/
│   ├── components/
│   │   ├── ui/            # ShadCN
│   │   ├── layouts/
│   │   └── issues/
│   ├── entrypoints/application.tsx
│   ├── lib/utils.ts
│   ├── pages/
│   └── types/index.d.ts
```

---

## Verification

```bash
bin/rails test
```

Manual checks:
1. Login with `admin@garnet.local` / `password123`
2. Navigate issues list, open detail, switch tabs
3. Add comment, verify keyboard navigation
4. Test all settings forms

---

## Effort Estimate

| Task | Hours |
|------|-------|
| BE1 | 1-2 |
| BE2 | 3-4 |
| FE1 | 2-3 |
| FE2 | 3-4 |
| FE3 | 6-8 |
| FE4 | 4-5 |
| Cleanup | 2-3 |
| **Total** | **21-29** |
