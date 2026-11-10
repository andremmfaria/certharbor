import './zodConfig'
import { getSessionQueryKey } from '@andremmfaria/certharbor-client/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter } from 'react-router'
import { App } from './App'
import { installClient } from './api/client'
import { dropUserData } from './api/invalidation'
import { createQueryClient } from './api/queryClient'
import { installPreloadErrorReload } from './preloadError'
import { appRoutes } from './routes/appRoutes'
import { applyTheme, storedThemePreference, watchSystemTheme } from './theme'
import './styles/themes.css'
import './styles/base.css'
import './styles/components.css'
import './styles/pages.css'
import './styles/utilities.css'
import './styles/spa.css'

const queryClient = createQueryClient()
// Vite's base, so the basename can never disagree with where the bundle is served.
const router = createBrowserRouter(appRoutes, { basename: import.meta.env.BASE_URL.replace(/\/$/, '') })

installClient({
  clearCache: () => {
    dropUserData(queryClient)
    void queryClient.invalidateQueries({ queryKey: getSessionQueryKey() })
  },
  redirectToLogin: () => {
    void router.navigate('/login', { replace: true, state: { from: router.state.location } })
  },
})
installPreloadErrorReload()
applyTheme(storedThemePreference())
watchSystemTheme()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App queryClient={queryClient} router={router} />
  </StrictMode>,
)
