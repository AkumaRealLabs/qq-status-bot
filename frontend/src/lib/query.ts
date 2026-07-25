import type { QueryClient } from '@tanstack/react-query'

export function invalidateMonitor(qc: QueryClient) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: ['upstreams'] }),
    qc.invalidateQueries({ queryKey: ['balances'] }),
    qc.invalidateQueries({ queryKey: ['cost-bindings'] }),
  ])
}
