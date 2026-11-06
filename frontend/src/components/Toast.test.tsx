import { act, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '../test/render'
import { useToast } from './Toast'

function Trigger() {
  const toast = useToast()
  return (
    <>
      <button onClick={() => toast.success('Saved')}>ok</button>
      <button onClick={() => toast.error('Failed')}>err</button>
    </>
  )
}

describe('toast', () => {
  afterEach(() => vi.useRealTimers())

  it('shows a success and an error against the ported alert rules, then dismisses them', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderWithProviders(<Trigger />)
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    await user.click(screen.getByText('ok'))
    await user.click(screen.getByText('err'))

    expect(screen.getByText('Saved').closest('.alert')).toHaveClass('alert-ok')
    expect(screen.getByText('Failed').closest('.alert')).toHaveClass('alert-err')

    await act(() => vi.advanceTimersByTimeAsync(5100))
    expect(screen.queryByText('Saved')).toBeNull()
  })

  it('can be dismissed by hand', async () => {
    renderWithProviders(<Trigger />)
    await userEvent.click(screen.getByText('err'))
    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByText('Failed')).toBeNull()
  })
})
