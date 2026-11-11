import { useProvisioners } from '../api/provisioners'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'

export function ProvisionersPage() {
  const provisioners = useProvisioners()

  return (
    <section data-testid="page-provisioners">
      <div className="page-header">
        <div>
          <div className="breadcrumb">ca &middot; provisioners</div>
          <h1>Provisioners</h1>
        </div>
      </div>

      {provisioners.isPending ? (
        <Skeleton rows={3} label="Loading provisioners" />
      ) : provisioners.isError ? (
        <ErrorState problem={provisioners.error} onRetry={() => void provisioners.refetch()} />
      ) : !provisioners.data || provisioners.data.length === 0 ? (
        <div className="card">
          <div className="card-body muted-center-p24">No provisioners or CA is unavailable</div>
        </div>
      ) : (
        <div className="card">
          <div className="card-table-wrap tbl-wrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                </tr>
              </thead>
              <tbody>
                {provisioners.data.map((prov) => (
                  <tr key={prov.name}>
                    <td data-label="Name">
                      <strong>{prov.name}</strong>
                    </td>
                    <td data-label="Type">{prov.type}</td>
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
