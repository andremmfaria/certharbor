import { QueryClient } from '@tanstack/react-query'
import { isClientError } from './errors'

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (failureCount, error) => failureCount < 2 && !isClientError(error),
        refetchOnWindowFocus: false,
        staleTime: 30_000,
        throwOnError: false,
      },
    },
  })
}
