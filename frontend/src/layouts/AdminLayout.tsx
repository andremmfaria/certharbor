import { useLayoutEffect } from 'react'
import { NavLink, Outlet } from 'react-router'
import { useSignOut } from '../auth/useSignOut'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { AdminIcon } from '../components/icons'
import { Skeleton } from '../components/Skeleton'
import { useVisibleNavigation } from '../hooks/useNavigation'
import { adminNavigation } from '../navigation'
import '../styles/admin.css'

function ShieldMark({ size }: { size: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 2 3 7v5c0 5 3.5 9.5 9 11 5.5-1.5 9-6 9-11V7l-9-5z" />
    </svg>
  )
}

export function Component() {
  const nav = useVisibleNavigation(adminNavigation)
  const signOut = useSignOut()

  // The ported stylesheets key the whole layout off this body class.
  useLayoutEffect(() => {
    document.body.classList.add('admin-mode')
    return () => document.body.classList.remove('admin-mode')
  }, [])

  return (
    <>
      <header className="admin-topnav">
        <NavLink to="/admin" className="admin-topbrand" aria-label="Step-CA admin home">
          <span className="admin-topbrand-icon">
            <ShieldMark size={16} />
          </span>
          <span>Admin</span>
        </NavLink>
        <nav className="admin-topmenu" aria-label="Admin navigation">
          {nav.sections.map((section) => (
            <details key={section.title} className="admin-dd">
              <summary>{section.title}</summary>
              <div className="admin-dd-menu">
                {section.items.map((item) => (
                  <NavLink key={item.to} to={item.to} end={item.end}>
                    {item.label}
                  </NavLink>
                ))}
              </div>
            </details>
          ))}
          <details className="admin-dd admin-dd-right">
            <summary>Account</summary>
            <div className="admin-dd-menu">
              <NavLink to="/">Back to Application</NavLink>
              <button type="button" className="admin-danger-link" onClick={signOut}>
                Sign Out
              </button>
            </div>
          </details>
        </nav>
      </header>
      <div className="admin-shell">
        <aside className="admin-side">
          <div className="admin-side-header">
            <div className="admin-shield">
              <ShieldMark size={20} />
            </div>
            <div className="admin-title">
              <b>Admin Panel</b>
              <span>Step-CA · Control</span>
            </div>
          </div>
          <nav className="admin-nav">
            {nav.ready ? (
              nav.sections.map((section) => (
                <div key={section.title}>
                  <div className="admin-nav-section">{section.title}</div>
                  {section.items.map((item) => (
                    <NavLink
                      key={item.to}
                      to={item.to}
                      end={item.end}
                      className={({ isActive }) => (isActive ? 'active' : '')}
                    >
                      {item.icon ? <AdminIcon name={item.icon} /> : null}
                      <span>{item.label}</span>
                    </NavLink>
                  ))}
                </div>
              ))
            ) : (
              <Skeleton rows={6} label="Loading navigation" />
            )}
          </nav>
          <div className="admin-side-footer">
            <NavLink to="/">
              <AdminIcon name="back" size={14} />
              <span>Back to Application</span>
            </NavLink>
            <div className="logout-form">
              <button type="button" className="admin-danger-link" onClick={signOut}>
                <AdminIcon name="signout" size={14} />
                <span>Sign Out</span>
              </button>
            </div>
          </div>
        </aside>
        <main className="admin-main">
          <div className="admin-header-banner">
            <ShieldMark size={16} />
            <span>Restricted administrator area. Critical actions are recorded in the audit log.</span>
          </div>
          <ErrorBoundary scope="admin-layout">
            <Outlet />
          </ErrorBoundary>
        </main>
      </div>
    </>
  )
}
