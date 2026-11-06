import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { createMemoryRouter, type RouteObject } from 'react-router'
import { RouterProvider } from 'react-router/dom'
import { ToastProvider } from '../components/Toast'
import { createQueryClient } from '../api/queryClient'

export function renderWithProviders(ui: ReactElement, queryClient: QueryClient = createQueryClient()) {
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        <ToastProvider>{ui}</ToastProvider>
      </QueryClientProvider>,
    ),
  }
}

export function renderRoutes(
  routes: RouteObject[],
  initialEntries: string[],
  queryClient: QueryClient = createQueryClient(),
) {
  const router = createMemoryRouter(routes, { initialEntries })
  return {
    router,
    ...renderWithProviders(<RouterProvider router={router} />, queryClient),
  }
}
