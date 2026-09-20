import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { Toaster } from 'sonner'
import './i18n'
import './index.css'
import { App } from './App.tsx'
import { BASE_PATH } from './basePath'
import { ThemeProvider } from './components/theme-provider'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <BrowserRouter basename={BASE_PATH}>
        <App />
        <Toaster richColors closeButton position="bottom-right" />
      </BrowserRouter>
    </ThemeProvider>
  </StrictMode>,
)
