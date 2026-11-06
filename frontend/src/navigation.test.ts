import { describe, expect, it } from 'vitest'
import { adminNavigation, appNavigation, roleAllows, visibleSections } from './navigation'
import { ROLE_LEVELS } from './test/fakeApi'

const features = { acme: true }
const labels = (sections: ReturnType<typeof visibleSections>) =>
  sections.flatMap((s) => s.items.map((i) => i.label))

describe('visibleSections', () => {
  it('filters by the role ordering the server reports', () => {
    const viewer = labels(visibleSections(appNavigation, { role: 'viewer', levels: ROLE_LEVELS, features }))
    const manager = labels(visibleSections(appNavigation, { role: 'manager', levels: ROLE_LEVELS, features }))
    const admin = labels(visibleSections(appNavigation, { role: 'admin', levels: ROLE_LEVELS, features }))

    expect(viewer).not.toContain('Issue')
    expect(manager).toContain('Issue')
    expect(manager).not.toContain('Admin Panel')
    expect(admin).toContain('Admin Panel')
  })

  it('hides ACME entries when the feature is off', () => {
    const items = labels(
      visibleSections(appNavigation, { role: 'admin', levels: ROLE_LEVELS, features: { acme: false } }),
    )
    expect(items).not.toContain('LE Certificates')
  })

  it('hides everything for a role the server does not know', () => {
    expect(visibleSections(appNavigation, { role: 'root', levels: ROLE_LEVELS, features })).toEqual([])
    expect(visibleSections(appNavigation, { role: undefined, levels: ROLE_LEVELS, features })).toEqual([])
  })

  it('hides everything until levels are known', () => {
    expect(visibleSections(adminNavigation, { role: 'admin', levels: {}, features })).toEqual([])
  })

  it('compares by level, not by name', () => {
    expect(roleAllows('admin', 'viewer', ROLE_LEVELS)).toBe(true)
    expect(roleAllows('viewer', 'admin', ROLE_LEVELS)).toBe(false)
  })
})
