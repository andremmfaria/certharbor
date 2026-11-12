import { useState } from 'react'
import { useAdminConsoleCommands, useRunAdminConsoleCommand } from '../api/admin'
import { errorMessage } from '../api/errors'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

// The command picker never carries Name/Args: listAdminConsoleCommands
// narrows to {id, label, description} server-side (api/admin.go), so this
// page cannot leak the database DSN even if it tried.
export function ConsolePage() {
  const commands = useAdminConsoleCommands()
  const run = useRunAdminConsoleCommand()
  const toast = useToast()
  const [selected, setSelected] = useState<string>('')

  const items = commands.data ?? []
  const selectedCommand = items.find((c) => c.id === selected)

  const onRun = () => {
    if (!selected) return
    run.mutate(
      { body: { commandId: selected } },
      {
        onError: (e) => toast.error(errorMessage(e, 'Command failed to run.')),
      },
    )
  }

  return (
    <section data-testid="page-admin-console">
      <div className="page-header">
        <div>
          <div className="breadcrumb">admin &middot; console</div>
          <h1>Server console</h1>
        </div>
      </div>

      {commands.isPending ? (
        <Skeleton rows={3} label="Loading commands" />
      ) : commands.isError ? (
        <ErrorState problem={commands.error} onRetry={() => void commands.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState title="No commands">No console commands are available.</EmptyState>
      ) : (
        <div className="card">
          <div className="card-body cert-filter-grid">
            <div className="form-group">
              <label htmlFor="console-command">Command</label>
              <select id="console-command" value={selected} onChange={(e) => setSelected(e.target.value)}>
                <option value="">Select a command&hellip;</option>
                {items.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.label}
                  </option>
                ))}
              </select>
              {selectedCommand ? <div className="text-muted">{selectedCommand.description}</div> : null}
            </div>
            <div className="form-group">
              <button
                type="button"
                className="btn btn-primary"
                disabled={!selected || run.isPending}
                onClick={onRun}
              >
                {run.isPending ? 'Running…' : 'Run'}
              </button>
            </div>
          </div>
        </div>
      )}

      {run.data ? (
        <div className="card" data-testid="console-result">
          <div className="card-header">
            <code>{run.data.commandLine}</code>
            <span className={`badge ${run.data.success ? 'ok' : 'danger'}`}>
              {run.data.success ? 'success' : 'failed'}
            </span>
            {run.data.timedOut ? <span className="badge warn">timed out</span> : null}
          </div>
          <div className="card-body">
            <div className="page-actions">
              <span className="text-muted">exit code {run.data.exitCode}</span>
              <span className="text-muted">duration {run.data.duration}</span>
              {run.data.truncated ? <span className="badge warn">output truncated</span> : null}
            </div>
            <pre className="console-output">{run.data.output}</pre>
          </div>
        </div>
      ) : null}
    </section>
  )
}
