import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { CrmShell } from './crm/CrmShell'
import './styles.css'

class ErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { error: Error | null }
> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (this.state.error) {
      return (
        <div className="page">
          <div className="state">
            <div className="h">Something went wrong.</div>
            <button onClick={() => location.reload()}>Reload</button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}

const isCrm = location.pathname === '/crm' || location.pathname.startsWith('/crm/')

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>{isCrm ? <CrmShell /> : <App />}</ErrorBoundary>
  </React.StrictMode>,
)
