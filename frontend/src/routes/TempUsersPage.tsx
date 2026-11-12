import { useState } from 'react'
import { useCreateTemporaryUser, useDeleteTemporaryUser, useTemporaryUsers } from '../api/users'
import { errorMessage } from '../api/errors'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

const ROLES = ['viewer', 'manager', 'admin'] as const
type Role = (typeof ROLES)[number]

const PRESETS = [
  { id: '1h', label: '1 hour', ms: 60 * 60 * 1000 },
  { id: '1d', label: '1 day', ms: 24 * 60 * 60 * 1000 },
  { id: '7d', label: '7 days', ms: 7 * 24 * 60 * 60 * 1000 },
] as const

function statusBadge(status: string) {
  const cls = status === 'active' ? 'ok' : status === 'expired' ? 'warn' : 'danger'
  return <span className={`badge ${cls}`}>{status}</span>
}

export function TempUsersPage() {
  const temps = useTemporaryUsers()
  const createTemp = useCreateTemporaryUser()
  const deleteTemp = useDeleteTemporaryUser()
  const toast = useToast()

  const [role, setRole] = useState<Role>('viewer')
  const [note, setNote] = useState('')
  const [preset, setPreset] = useState<string>(PRESETS[0].id)
  const [createError, setCreateError] = useState<string | undefined>()
  const [credential, setCredential] = useState<{ username: string; passphrase: string } | undefined>()

  const onCreate = (e: React.FormEvent) => {
    e.preventDefault()
    setCreateError(undefined)
    const chosen = PRESETS.find((p) => p.id === preset) ?? PRESETS[0]
    const expiresAt = new Date(Date.now() + chosen.ms).toISOString()
    createTemp.mutate(
      { body: { role, note: note || undefined, expiresAt } },
      {
        onSuccess: (u) => {
          setCredential({ username: u.username, passphrase: u.passphrase })
          setNote('')
        },
        onError: (e) => setCreateError(errorMessage(e, 'Creating the temporary user failed.')),
      },
    )
  }

  const onDelete = (id: number, username: string) => {
    if (!window.confirm(`Delete the temporary user ${username}?`)) return
    deleteTemp.mutate(
      { path: { id } },
      {
        onSuccess: () => toast.success(`${username} deleted.`),
        onError: (e) => toast.error(errorMessage(e, 'Deleting the temporary user failed.')),
      },
    )
  }

  return (
    <section data-testid="page-admin-users-temp">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; temporary users</div>
          <h1>Temporary users</h1>
        </div>
      </div>

      {credential ? (
        <div className="alert alert-ok" role="status" data-testid="temp-user-credential">
          <strong>{credential.username}</strong> was created. One-time passphrase:{' '}
          <code>{credential.passphrase}</code> — it will not be shown again.{' '}
          <button type="button" className="btn-sm" onClick={() => setCredential(undefined)}>
            Dismiss
          </button>
        </div>
      ) : null}

      <div className="card">
        <div className="card-header">
          <h3>Add temporary user</h3>
        </div>
        <div className="card-body">
          <form onSubmit={onCreate} noValidate className="form-grid">
            <div className="form-group">
              <label htmlFor="temp-role">Role</label>
              <select id="temp-role" value={role} onChange={(e) => setRole(e.target.value as Role)}>
                {ROLES.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            </div>
            <div className="form-group">
              <label htmlFor="temp-expires">Expires in</label>
              <select id="temp-expires" value={preset} onChange={(e) => setPreset(e.target.value)}>
                {PRESETS.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="form-group">
              <label htmlFor="temp-note">Note</label>
              <input id="temp-note" type="text" value={note} onChange={(e) => setNote(e.target.value)} />
            </div>
            {createError ? (
              <div className="error" role="alert" data-testid="create-temp-user-error">
                {createError}
              </div>
            ) : null}
            <div className="page-actions">
              <button className="btn" type="submit" disabled={createTemp.isPending}>
                Create temporary user
              </button>
            </div>
          </form>
        </div>
      </div>

      {temps.isPending ? (
        <Skeleton rows={4} label="Loading temporary users" />
      ) : temps.isError ? (
        <ErrorState problem={temps.error} onRetry={() => void temps.refetch()} />
      ) : !temps.data || temps.data.length === 0 ? (
        <EmptyState title="No temporary users">No temporary users have been created.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Username</th>
                  <th>Role</th>
                  <th>Status</th>
                  <th>Note</th>
                  <th>Created</th>
                  <th>Expires</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {temps.data.map((u) => (
                  <tr key={u.id}>
                    <td data-label="Username">
                      <strong>{u.username}</strong>
                    </td>
                    <td data-label="Role">{u.role}</td>
                    <td data-label="Status">{statusBadge(u.status)}</td>
                    <td data-label="Note" className="text-muted">
                      {u.note}
                    </td>
                    <td data-label="Created" className="td-muted-ts">
                      {new Date(u.createdAt).toLocaleString()}
                    </td>
                    <td data-label="Expires" className="td-muted-ts">
                      {u.expiresAt ? new Date(u.expiresAt).toLocaleString() : '—'}
                    </td>
                    <td data-label="Actions">
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        disabled={deleteTemp.isPending}
                        onClick={() => onDelete(u.id, u.username)}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  )
}
