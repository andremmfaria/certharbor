import { useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router'
import { useSignOut } from '../auth/useSignOut'
import { useAuthenticatedUser } from '../auth/useAuthenticatedUser'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { AppIcon } from '../components/icons'
import { Skeleton } from '../components/Skeleton'
import { useVisibleNavigation } from '../hooks/useNavigation'
import { appNavigation, type NavSection } from '../navigation'
import type { AppIconName } from '../components/icons'

function LockMark() {
  return (
    <svg viewBox="0 0 16 16" aria-hidden="true">
      <rect x="3" y="7" width="10" height="8" rx="2" />
      <path d="M5 7V5a3 3 0 0 1 6 0v2" />
    </svg>
  )
}

function SidebarGroup({ section }: { section: NavSection<AppIconName> }) {
  const { pathname } = useLocation()
  const containsActive = section.items.some((i) => pathname === i.to || pathname.startsWith(i.to + '/'))
  const [toggled, setToggled] = useState<boolean | undefined>(undefined)
  const open = toggled ?? containsActive
  if (!section.group) return null
  return (
    <div className="nav-group">
      <div
        className={`nav-group-toggle${open ? ' open' : ''}`}
        role="button"
        tabIndex={0}
        aria-expanded={open}
        onClick={() => setToggled(!open)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            setToggled(!open)
          }
        }}
      >
        <AppIcon name={section.group.icon} />
        {section.group.label}
        <span className="nav-arrow">&#9658;</span>
      </div>
      <div className={`nav-group-items${open ? ' open' : ''}`}>
        {section.items.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={({ isActive }) => (isActive ? 'active' : '')}
          >
            {item.label}
          </NavLink>
        ))}
      </div>
    </div>
  )
}

export function AppLayout() {
  const user = useAuthenticatedUser()
  const nav = useVisibleNavigation(appNavigation)
  const signOut = useSignOut()
  const [collapsed, setCollapsed] = useState(false)

  return (
    <>
      <header className="mobile-topnav">
        <NavLink to="/" className="mobile-brand" aria-label="Step-CA home">
          <span className="mobile-brand-icon">
            <LockMark />
          </span>
          <span>Step-CA</span>
        </NavLink>
        <nav className="mobile-menu" aria-label="Main navigation">
          {nav.sections.map((section) =>
            section.group ? (
              <details key={section.title} className="mobile-dd">
                <summary>{section.group.label}</summary>
                <div className="mobile-dd-menu">
                  {section.items.map((item) => (
                    <NavLink key={item.to} to={item.to} end={item.end}>
                      {item.label}
                    </NavLink>
                  ))}
                </div>
              </details>
            ) : (
              section.items.map((item) => (
                <NavLink key={item.to} to={item.to} end={item.end}>
                  {item.label}
                </NavLink>
              ))
            ),
          )}
          <button type="button" className="danger-link" onClick={signOut}>
            Sign Out
          </button>
        </nav>
      </header>
      <button
        type="button"
        className="burger-float"
        aria-label="Open menu"
        title="Open menu"
        onClick={() => setCollapsed(false)}
      >
        <span />
        <span />
        <span />
      </button>
      <aside className={`sidebar${collapsed ? ' collapsed' : ''}`} id="sidebar">
        <div className="logo">
          <button
            type="button"
            className="burger"
            aria-label="Menu"
            title="Menu"
            onClick={() => setCollapsed(!collapsed)}
          >
            <span />
            <span />
            <span />
          </button>
          <div className="logo-icon">
            <LockMark />
          </div>
          <div className="logo-text">
            <h2>Step-CA</h2>
            <span>Certificate Manager</span>
          </div>
        </div>
        <nav className="nav">
          {nav.ready ? (
            nav.sections.map((section) => (
              <div key={section.title}>
                <div className="nav-section">{section.title}</div>
                {section.group ? (
                  <SidebarGroup section={section} />
                ) : (
                  section.items.map((item) => (
                    <NavLink
                      key={item.to}
                      to={item.to}
                      end={item.end}
                      className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}
                    >
                      {item.icon ? <AppIcon name={item.icon} /> : null}
                      {item.label}
                    </NavLink>
                  ))
                )}
              </div>
            ))
          ) : (
            <Skeleton rows={6} label="Loading navigation" />
          )}
          <div className="logout-form">
            <button type="button" className="nav-link danger-link" onClick={signOut}>
              <AppIcon name="signout" />
              Sign Out
            </button>
          </div>
        </nav>
        <div className="sidebar-footer">
          <div className="user-info">
            {user.username} &middot; <span className="sidebar-role">{user.role}</span>
          </div>
        </div>
      </aside>
      <main className="main">
        <ErrorBoundary scope="app-layout">
          <Outlet />
        </ErrorBoundary>
      </main>
    </>
  )
}
