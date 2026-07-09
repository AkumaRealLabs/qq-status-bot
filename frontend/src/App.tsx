import { lazy, Suspense } from 'react'
import { ShellLoading } from '@/components/layout'

const PublicApp = lazy(() => import('@/PublicApp'))
const AdminApp = lazy(() => import('@/AdminApp'))

function isAdminPath(pathname: string) {
  return pathname === '/admin' || pathname.startsWith('/admin/')
}

/** 薄路由：公开 / 与管理 /admin/* 加载完全独立的应用图。 */
export default function App() {
  const admin = isAdminPath(location.pathname)
  return <Suspense fallback={<ShellLoading />}>{admin ? <AdminApp /> : <PublicApp />}</Suspense>
}
