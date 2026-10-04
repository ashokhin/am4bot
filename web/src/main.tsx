import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider, createBrowserRouter } from 'react-router-dom'
import { Toaster } from 'sonner'
import './i18n'
import './index.css'
import { App } from './App.tsx'
import { BASE_PATH } from './basePath'
import { ThemeProvider } from './components/theme-provider'

// A data router (rather than BrowserRouter) so pages can block navigation
// while they hold unsaved changes - see useBlocker in NodeFormPage.
const router = createBrowserRouter([{ path: '*', element: <App /> }], { basename: BASE_PATH })

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <RouterProvider router={router} />
      <Toaster richColors closeButton position="bottom-right" />
    </ThemeProvider>
  </StrictMode>,
)
