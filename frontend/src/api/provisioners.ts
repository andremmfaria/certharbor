import { listProvisionersOptions } from '@certharbor/client/react-query'
import { useQuery } from '@tanstack/react-query'

// No pagination: the operation returns the CA's full provisioner list, the
// same shape the template route rendered with no paging either.
export function useProvisioners() {
  return useQuery(listProvisionersOptions())
}
