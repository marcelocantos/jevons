import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { setNow } from './clock'
import { installErrorReport } from './errorReport'
import { applyTheme, readThemePref } from './theme'
import './cockpit.css'
import App from './App.tsx'

const freeze = (globalThis as { __JEVONS_CLOCK_NOW?: unknown }).__JEVONS_CLOCK_NOW
if (freeze != null && freeze !== false) setNow(Number(freeze))
applyTheme(readThemePref())
// 🎯T505: before render, so a throw during the first paint is journaled too.
installErrorReport()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
