import { defineConfig, type PluginOption } from 'vite'
import RubyPlugin from 'vite-plugin-ruby'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// Workarround for bug in vite-plugin-ruby. Error during app boot.
const rubyPlugins: PluginOption[] = RubyPlugin().map((plugin) => {
  if (plugin && typeof plugin === 'object' && 'config' in plugin && typeof plugin.config === 'function') {
    const originalConfig = plugin.config
    return {
      ...plugin,
      config(this: unknown, userConfig, env) {
        return originalConfig.call(this ?? {}, userConfig, env)
      },
    }
  }

  return plugin
})

export default defineConfig({
  plugins: [
    ...rubyPlugins,
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'app/frontend'),
    },
  },
})
