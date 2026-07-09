import { lazy, Suspense } from 'react'
import { ShellLoading } from '@/components/layout'

const PublicApp = lazy(() => import('@/PublicApp'))
const AdminApp = lazy(() => import('@/AdminApp'))

function isAdminPath(pathname: string) {
  return pathname === '/admin' || pathname.startsWith('/admin/')
}

/** Thin router: public / and admin /admin/* load completely separate application graphs. */
export default function App() {
  const admin = isAdminPath(location.pathname)
  return <Suspense fallback={<ShellLoading />}>{admin ? <AdminApp /> : <PublicApp />}</Suspense>
}
