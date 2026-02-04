# frozen_string_literal: true

Rails.application.routes.draw do
  # Define your application routes per the DSL in https://guides.rubyonrails.org/routing.html

  # Reveal health status on /up that returns 200 if the app boots with no exceptions, otherwise 500.
  # Can be used by load balancers and uptime monitors to verify that the app is live.
  get "up" => "rails/health#show", as: :rails_health_check

  # Deep health check including database and event store connectivity
  get "health" => "health#show", as: :health_check

  # Render dynamic PWA files from app/views/pwa/* (remember to link manifest in application.html.erb)
  # get "manifest" => "rails/pwa#manifest", as: :pwa_manifest
  # get "service-worker" => "rails/pwa#service_worker", as: :pwa_service_worker

  # Defines the root path route ("/")
  root "home#index"
  get "login", to: "sessions#new"
  post "login", to: "sessions#create"
  resource :session
  resource :registration, only: [ :new, :create ]
  resource :user_settings, only: [ :show, :update ], controller: "user_settings"
  resources :passwords, param: :token

  scope "/:org_slug" do
    resources :issues, only: [ :index ]
    resources :mentions, only: [ :index ]
    resources :projects, only: [ :index, :new, :create, :show, :update ], param: :slug do
      resources :issues, only: [ :show ], param: :number do
        member do
          patch :resolve
          patch :unresolve
          patch :assign
        end
        resources :comments, only: [ :create, :destroy ]
      end

      resources :keys, only: [ :create, :destroy ], controller: "project_keys" do
        member do
          patch :rotate
        end
      end
    end

    scope "/settings" do
      resource :organization, only: [ :show, :update ], controller: "organization_settings"
      resources :members, only: [ :index, :create, :destroy ], controller: "organization_members"
      resources :integrations, only: [ :index, :new, :create, :edit, :update, :destroy ]
      resource :user, only: [ :show, :update ], controller: "user_settings"
    end
    get "settings", to: redirect("/%{org_slug}/settings/organization")
  end

  # API Routes (Migrated from Backend)




  # API Routes
  namespace :api do
    namespace :v1 do
      scope "/:org_slug" do
        get "issues", to: "issues#index", as: :issues
        get "issues/:id", to: "issues#show", as: :issue
        patch "issues/:id/resolve", to: "issues#resolve", as: :resolve_issue
        patch "issues/:id/unresolve", to: "issues#unresolve", as: :unresolve_issue

        resources :projects, param: :project_slug, only: [ :index, :show, :update ] do
          member do
            get "keys", to: "project_keys#index"
            post "keys", to: "project_keys#create"
            put "keys/:id", to: "project_keys#update"
            delete "keys/:id", to: "project_keys#destroy"
          end
        end
      end

      post "auth/login", to: "auth#login"
      post "auth/register", to: "auth#register"
      get "auth/me", to: "auth#me"
    end
  end
end
