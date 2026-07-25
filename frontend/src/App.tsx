import { lazy, Suspense } from 'react'
import { ShellLoading } from '@/components/layout'

const AdminApp = lazy(() => import('@/AdminApp'))

function adminPath(pathname: string) {
  if (pathname === '/scheduler' || pathname === '/admin/scheduler') return '/admin/costs'
  if (pathname === '/' || pathname === '/admin' || pathname === '/status' || pathname === '/admin/status') return '/admin/balances'
  if (pathname.startsWith('/admin/')) return pathname
  return '/admin/balances'
}

export default function App() {
  const path = adminPath(location.pathname)
  if (path !== location.pathname) window.history.replaceState(null, '', path)
  return <Suspense fallback={<ShellLoading />}><AdminApp /></Suspense>
}
