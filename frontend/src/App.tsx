import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from 'react-router/dom'
import type { DataRouter } from 'react-router'
import type { QueryClient } from '@tanstack/react-query'
import { ToastProvider } from './components/Toast'

export function App({ queryClient, router }: { queryClient: QueryClient; router: DataRouter }) {
  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <RouterProvider router={router} />
      </ToastProvider>
    </QueryClientProvider>
  )
}
