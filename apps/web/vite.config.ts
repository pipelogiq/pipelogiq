import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, __dirname, 'PIPELOGIQ_DEV_API_TARGET')
  const configuredTarget = env.PIPELOGIQ_DEV_API_TARGET?.trim() || 'http://localhost:8080'
  const invalidTarget = () => new Error(
    'PIPELOGIQ_DEV_API_TARGET must be an http:// or https:// URL with no credentials, query or fragment, and a path of / or /api.',
  )

  let apiTarget: URL
  try {
    apiTarget = new URL(configuredTarget)
  } catch {
    throw invalidTarget()
  }
  if (
    !/^https?:\/\//i.test(configuredTarget) ||
    !['http:', 'https:'].includes(apiTarget.protocol) ||
    apiTarget.username || apiTarget.password ||
    /^https?:\/\/[^/?#]*@/i.test(configuredTarget) ||
    configuredTarget.includes('?') || configuredTarget.includes('#') ||
    !['/', '/api', '/api/'].includes(apiTarget.pathname)
  ) {
    throw invalidTarget()
  }
  const httpTarget = apiTarget.origin + apiTarget.pathname.replace(/\/$/, '')
  const websocketTarget = `${apiTarget.protocol === 'https:' ? 'wss:' : 'ws:'}//${apiTarget.host}`

  return {
    plugins: [react()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    server: {
      port: 3300,
      proxy: {
        '/api': {
          target: httpTarget,
          changeOrigin: true,
          rewrite: (requestPath) => requestPath.replace(/^\/api/, ''),
        },
        '/ws': {
          target: websocketTarget,
          ws: true,
        },
      },
    },
  }
})
