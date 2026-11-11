import { listHistoryOptions } from '@certharbor/client/react-query'
import { useQuery } from '@tanstack/react-query'
import type { ListParams } from '../hooks/useListParams'

// Read-only list, same shape as useCertificates: URL params map straight onto
// the operation's own query params. `action` is repeatable, so it is passed
// as an array rather than a single string.
export function useHistory(
  params: Pick<ListParams, 'page' | 'pageSize'>,
  action: string[] | undefined,
  cert: string | undefined,
) {
  return useQuery(
    listHistoryOptions({
      query: {
        page: params.page,
        pageSize: params.pageSize,
        action: action && action.length > 0 ? action : undefined,
        cert: cert || undefined,
      },
    }),
  )
}
