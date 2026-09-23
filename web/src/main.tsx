import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import './styles.css'

const element = document.getElementById('root')
if (!element) throw new Error('Root element is missing')
createRoot(element).render(<React.StrictMode><App /></React.StrictMode>)
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => { void navigator.serviceWorker.register('/sw.js') })
}
