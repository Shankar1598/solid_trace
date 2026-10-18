import { defineConfig } from 'vitest/config'
import path from 'path'

// Vitest runs the frontend's plain modules (no rendering, no Rails), so it
// leaves out vite.config.ts and its Ruby plugin.
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, 'app/frontend'),
    },
  },
  test: {
    include: ['app/frontend/**/*.test.ts'],
    environment: 'node',
  },
})
