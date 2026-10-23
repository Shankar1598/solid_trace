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
  resources :api_tokens, only: [ :create, :destroy ]
  # The MCP server (McpController).
  post "mcp", to: "mcp#create", as: :mcp, format: false
  match "mcp", to: "mcp#method_not_allowed", via: [ :get, :delete ], as: nil, format: false
  resources :passwords, param: :token
  # An Invite link, where an Invited member sets a password (InvitesController).
  get "invites/:token", to: "invites#edit", as: :invite
  put "invites/:token", to: "invites#update"
  # Where a user in no Organization makes one (OrganizationsController).
  resources :organizations, only: [ :new, :create ]

  scope "/:org_slug" do
    get "overview", to: "overview#show", as: :overview
    resources :issues, only: [ :index ] do
      collection do
        patch :status, action: :update_status
      end
    end
    get "logs", to: "logs#start", as: :logs
    resources :dashboards, only: [ :index, :show, :create, :update, :destroy ] do
      member do
        patch :layout
      end
      resources :charts, only: [ :create, :update, :destroy ], controller: "dashboard_charts" do
        collection do
          post :preview
        end
        member do
          post :duplicate
          get :data
        end
      end
      resources :filters, only: [], controller: "dashboard_filters", param: :name do
        member do
          get :values
        end
      end
    end
    resources :monitors do
      collection do
        post :preview
      end
      member do
        patch :enable
        patch :disable
        post :run
      end
    end
    resources :mentions, only: [ :index ]
    get "organization_users/search", to: "organization_users#search", as: :organization_users_search
    resources :projects, only: [ :index, :new, :create, :show, :update, :destroy ], param: :slug do
      resources :issues, only: [ :show ], param: :number do
        member do
          patch :resolve
          patch :unresolve
          patch :assign
        end
        resources :comments, only: [ :create, :destroy ]
      end

      resources :logs, only: [ :index, :show ] do
        collection do
          get :query
          get :validate
          get :histogram
          get :readable_sql
          post :sql
          post :test, action: :send_test
        end
      end

      resources :keys, only: [ :create, :destroy ], controller: "project_keys" do
        member do
          patch :rotate
        end
      end

      resource :log_transform, only: [ :show, :update ] do
        post :check
        post :preview
        post :restore
      end
    end

    scope "/settings" do
      resource :organization, only: [ :show, :update ], controller: "organization_settings"
      resources :organization_users, only: [ :index, :create, :update, :destroy ]
      resources :integrations, only: [ :index, :new, :create, :edit, :update, :destroy ]
      resource :user, only: [ :show, :update ], controller: "user_settings"
      # The install's email, for instance admins (EmailSettingsController).
      resource :email_settings, only: [ :show, :update ], path: "email", controller: "email_settings" do
        post :send_test, path: "test"
      end
    end
    get "settings", to: redirect("/%{org_slug}/settings/organization")
  end
end
