Rails.application.routes.draw do
  # Authentication
  get "login", to: "sessions#new"
  post "login", to: "sessions#create"
  delete "logout", to: "sessions#destroy"
  get "signup", to: "registrations#new"
  post "signup", to: "registrations#create"

  scope "/:org_slug" do
    resources :issues, only: [:index, :show]
    resources :projects, only: [:index, :new, :create, :show]

    get "settings/organization", to: "organizations#edit", as: :edit_organization
    patch "settings/organization", to: "organizations#update"
    post "settings/organization/invite", to: "organizations#invite", as: :invite_organization_user

    get "settings/user", to: "users#edit", as: :edit_user
    patch "settings/user", to: "users#update"
  end

  # Keep root as dashboard for now, but maybe redirect to org dashboard later
  root "dashboard#index"

  # Define your application routes per the DSL in https://guides.rubyonrails.org/routing.html

  namespace :api do
    scope "0" do
      # Sentry uses /api/0/ as the prefix usually, but we can map it
      resources :projects do
        member do
          post "store", to: "v1/ingest#store"
          post "envelope", to: "v1/ingest#envelope"
        end
      end
    end

    # Also support /api/:project_id/store directly if needed, but Sentry usually does /api/:id/store/
    post "/:project_id/store", to: "v1/ingest#store", as: :ingest_store
    post "/:project_id/envelope", to: "v1/ingest#envelope", as: :ingest_envelope

    # Management API
    namespace :v1 do
      scope "/:org_slug" do
        get "issues", to: "issues#index", as: :issues
        get "issues/:id", to: "issues#show", as: :issue
        patch "issues/:id/resolve", to: "issues#resolve", as: :resolve_issue
        patch "issues/:id/unresolve", to: "issues#unresolve", as: :unresolve_issue

        resources :projects, param: :project_slug, only: [:index, :show, :update] do
          member do
            get "keys", to: "project_keys#index"
            post "keys", to: "project_keys#create"
            put "keys/:id", to: "project_keys#update"
            delete "keys/:id", to: "project_keys#destroy"
          end
        end

        post "invite", to: "organization_users#create"
      end

      post "auth/login", to: "auth#login"
      post "auth/register", to: "auth#register"
      get "auth/me", to: "auth#me"

      # get "debug/error", to: "debug#trigger_error"
    end
  end

  # Reveal health status on /up that returns 200 if the app boots with no exceptions, otherwise 500.
  # Can be used by load balancers and uptime monitors to verify that the app is live.
  get "up" => "rails/health#show", as: :rails_health_check

  # Render dynamic PWA files from app/views/pwa/* (remember to link manifest in application.html.erb)
  # get "manifest" => "rails/pwa#manifest", as: :pwa_manifest
  # get "service-worker" => "rails/pwa#service_worker", as: :pwa_service_worker

  # Defines the root path route ("/")
  # root "posts#index"
end
