import { useConfig } from '../api/queries'
import { useAuthenticatedUser } from '../auth/useAuthenticatedUser'
import { visibleSections, type NavSection } from '../navigation'

// Items stay hidden until the config (and with it roleLevels) has loaded:
// showing everything and then retracting it would flash controls the user
// cannot use.
export function useVisibleNavigation<Icon extends string>(sections: readonly NavSection<Icon>[]) {
  const user = useAuthenticatedUser()
  const config = useConfig(true)
  if (!config.data) return { ready: false as const, sections: [] as NavSection<Icon>[] }
  return {
    ready: true as const,
    sections: visibleSections(sections, {
      role: user.role,
      levels: config.data.roleLevels,
      features: { acme: config.data.acmeEnabled },
    }),
  }
}
