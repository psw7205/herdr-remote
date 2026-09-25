// Global CSS first, so component modules imported through App come later
// in the cascade and win at equal specificity.
import './tokens.css'
import './base.css'
import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'

const element = document.getElementById('root')
if (!element) throw new Error('Root element is missing')
createRoot(element).render(<React.StrictMode><App /></React.StrictMode>)
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => { void navigator.serviceWorker.register('/sw.js') })
}
