import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { setNow } from './clock'
import { applyTheme, readThemePref } from './theme'
import './cockpit.css'
import App from './App.tsx'

const freeze = (globalThis as { __JEVONS_CLOCK_NOW?: unknown }).__JEVONS_CLOCK_NOW
if (freeze != null && freeze !== false) setNow(Number(freeze))
applyTheme(readThemePref())

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
