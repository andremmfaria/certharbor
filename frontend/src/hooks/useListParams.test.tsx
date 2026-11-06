import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, useLocation } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useListParams } from './useListParams'

function setup(initial: string) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <MemoryRouter initialEntries={[initial]}>{children}</MemoryRouter>
  )
  return renderHook(
    () => {
      const list = useListParams()
      return { list, search: useLocation().search }
    },
    { wrapper },
  )
}

describe('useListParams', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('reads the URL as the single source of truth, with defaults', () => {
    const { result } = setup('/?page=3&pageSize=50&q=web&sort=name&order=desc')
    expect(result.current.list.params).toEqual({
      page: 3,
      pageSize: 50,
      q: 'web',
      sort: 'name',
      order: 'desc',
    })

    const bare = setup('/').result.current.list.params
    expect(bare).toEqual({ page: 1, pageSize: 25, q: '', sort: undefined, order: undefined })
  })

  it('ignores nonsense values', () => {
    expect(setup('/?page=-2&pageSize=abc&order=sideways').result.current.list.params).toMatchObject({
      page: 1,
      pageSize: 25,
      order: undefined,
    })
  })

  it('omits defaults from the address', () => {
    const { result } = setup('/?page=4')
    act(() => result.current.list.setPage(1))
    expect(result.current.search).toBe('')
    act(() => result.current.list.setPage(2))
    expect(result.current.search).toBe('?page=2')
  })

  it('debounces the search by 300 ms and resets paging', () => {
    const { result } = setup('/?page=4')

    act(() => result.current.list.setQuery('w'))
    act(() => result.current.list.setQuery('we'))
    act(() => void vi.advanceTimersByTime(299))
    expect(result.current.search).toBe('?page=4')

    act(() => void vi.advanceTimersByTime(1))
    expect(result.current.search).toBe('?q=we')
    expect(result.current.list.params.q).toBe('we')
  })

  it('resets paging when the page size or sort changes', () => {
    const { result } = setup('/?page=4')
    act(() => result.current.list.setSort('name', 'asc'))
    expect(new URLSearchParams(result.current.search).get('page')).toBeNull()
    expect(new URLSearchParams(result.current.search).get('sort')).toBe('name')
  })
})
