import { useNavigate } from 'react-router'
import { useLogout } from '../api/queries'

export function useSignOut(): () => void {
  const logout = useLogout()
  const navigate = useNavigate()
  return () => logout.mutate({}, { onSettled: () => void navigate('/login', { replace: true }) })
}
