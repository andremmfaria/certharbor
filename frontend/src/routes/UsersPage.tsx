import { useState } from 'react'
import { useCreateUser, useDeleteUser, useResetUserPassword, useUpdateUser, useUsers } from '../api/users'
import { errorMessage } from '../api/errors'
import { useAuthenticatedUser } from '../auth/useAuthenticatedUser'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'
import { useListParams } from '../hooks/useListParams'

const ROLES = ['viewer', 'manager', 'admin'] as const
type Role = (typeof ROLES)[number]

function activeBadge(isActive: boolean) {
  return <span className={`badge ${isActive ? 'ok' : 'danger'}`}>{isActive ? 'active' : 'disabled'}</span>
}

export function UsersPage() {
  const { params, qInput, setQuery, setPage } = useListParams()
  const self = useAuthenticatedUser()
  const users = useUsers(params, params.q)
  const createUser = useCreateUser()
  const updateUser = useUpdateUser()
  const deleteUser = useDeleteUser()
  const resetPassword = useResetUserPassword()
  const toast = useToast()

  const [newUsername, setNewUsername] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newRole, setNewRole] = useState<Role>('viewer')
  const [createError, setCreateError] = useState<string | undefined>()

  const onCreate = (e: React.FormEvent) => {
    e.preventDefault()
    setCreateError(undefined)
    createUser.mutate(
      { body: { username: newUsername, password: newPassword, role: newRole } },
      {
        onSuccess: (u) => {
          toast.success(`${u.username} created.`)
          setNewUsername('')
          setNewPassword('')
          setNewRole('viewer')
        },
        onError: (e) => setCreateError(errorMessage(e, 'Creating the user failed.')),
      },
    )
  }

  const onRoleChange = (id: number, role: Role) =>
    updateUser.mutate(
      { path: { id }, body: { role } },
      {
        onSuccess: () => toast.success('Role updated.'),
        onError: (e) => toast.error(errorMessage(e, 'Updating the role failed.')),
      },
    )

  const onToggleActive = (id: number, isActive: boolean) =>
    updateUser.mutate(
      { path: { id }, body: { isActive: !isActive } },
      {
        onSuccess: () => toast.success(isActive ? 'User disabled.' : 'User enabled.'),
        onError: (e) => toast.error(errorMessage(e, 'Updating the account failed.')),
      },
    )

  const onDelete = (id: number, username: string) => {
    if (!window.confirm(`Delete ${username}? This cannot be undone.`)) return
    deleteUser.mutate(
      { path: { id } },
      {
        onSuccess: () => toast.success(`${username} deleted.`),
        onError: (e) => toast.error(errorMessage(e, 'Deleting the user failed.')),
      },
    )
  }

  const onResetPassword = (id: number, username: string) => {
    const newPwd = window.prompt(`New password for ${username}:`)
    if (!newPwd) return
    resetPassword.mutate(
      { path: { id }, body: { newPassword: newPwd } },
      {
        onSuccess: () => toast.success(`${username}'s password was reset.`),
        onError: (e) => toast.error(errorMessage(e, 'Resetting the password failed.')),
      },
    )
  }

  return (
    <section data-testid="page-admin-users">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; users</div>
          <h1>Users</h1>
        </div>
      </div>

      <div className="card cert-filter-card">
        <div className="card-body cert-filter-grid">
          <div className="form-group">
            <label htmlFor="users-search">Search</label>
            <input
              id="users-search"
              type="text"
              placeholder="Username"
              value={qInput}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <h3>Add user</h3>
        </div>
        <div className="card-body">
          <form onSubmit={onCreate} noValidate className="form-grid">
            <div className="form-group">
              <label htmlFor="new-username">Username</label>
              <input
                id="new-username"
                type="text"
                required
                value={newUsername}
                onChange={(e) => setNewUsername(e.target.value)}
              />
            </div>
            <div className="form-group">
              <label htmlFor="new-password">Password</label>
              <input
                id="new-password"
                type="password"
                required
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
              />
            </div>
            <div className="form-group">
              <label htmlFor="new-role">Role</label>
              <select id="new-role" value={newRole} onChange={(e) => setNewRole(e.target.value as Role)}>
                {ROLES.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            </div>
            {createError ? (
              <div className="error" role="alert" data-testid="create-user-error">
                {createError}
              </div>
            ) : null}
            <div className="page-actions">
              <button className="btn" type="submit" disabled={createUser.isPending}>
                Create user
              </button>
            </div>
          </form>
        </div>
      </div>

      {users.isPending ? (
        <Skeleton rows={5} label="Loading users" />
      ) : users.isError ? (
        <ErrorState problem={users.error} onRetry={() => void users.refetch()} />
      ) : !users.data?.items || users.data.items.length === 0 ? (
        <EmptyState title="No users">No users match the current filters.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Username</th>
                  <th>Role</th>
                  <th>Status</th>
                  <th>Last login</th>
                  <th>Last IP</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {users.data.items.map((u) => {
                  const isSelf = u.id === self.id
                  return (
                    <tr key={u.id}>
                      <td data-label="Username">
                        <strong>{u.username}</strong>
                      </td>
                      <td data-label="Role">
                        <select
                          aria-label={`Role for ${u.username}`}
                          value={u.role}
                          disabled={isSelf || updateUser.isPending}
                          onChange={(e) => onRoleChange(u.id, e.target.value as Role)}
                        >
                          {ROLES.map((r) => (
                            <option key={r} value={r}>
                              {r}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td data-label="Status">{activeBadge(u.isActive)}</td>
                      <td data-label="Last login" className="td-muted-ts">
                        {u.lastLogin ? new Date(u.lastLogin).toLocaleString() : '—'}
                      </td>
                      <td data-label="Last IP">{u.lastIp ? <code>{u.lastIp}</code> : '—'}</td>
                      <td data-label="Actions">
                        <button
                          type="button"
                          className="btn btn-ghost btn-sm"
                          disabled={isSelf || updateUser.isPending}
                          onClick={() => onToggleActive(u.id, u.isActive)}
                        >
                          {u.isActive ? 'Disable' : 'Enable'}
                        </button>{' '}
                        <button
                          type="button"
                          className="btn btn-ghost btn-sm"
                          disabled={resetPassword.isPending}
                          onClick={() => onResetPassword(u.id, u.username)}
                        >
                          Reset password
                        </button>{' '}
                        <button
                          type="button"
                          className="btn btn-ghost btn-sm"
                          disabled={isSelf || deleteUser.isPending}
                          onClick={() => onDelete(u.id, u.username)}
                        >
                          Delete
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {users.data && users.data.totalPages && users.data.totalPages > 1 ? (
        <div className="page-actions">
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={params.page <= 1}
            onClick={() => setPage(params.page - 1)}
          >
            Previous
          </button>
          <span className="breadcrumb">
            Page {users.data.page} of {users.data.totalPages}
          </span>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={params.page >= users.data.totalPages}
            onClick={() => setPage(params.page + 1)}
          >
            Next
          </button>
        </div>
      ) : null}
    </section>
  )
}
