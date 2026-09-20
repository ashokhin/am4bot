import { Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { AdminRoute } from './components/AdminRoute'
import { Layout } from './components/Layout'
import { NonAdminRoute } from './components/NonAdminRoute'
import { ProtectedRoute } from './components/ProtectedRoute'
import { AdminMetricsPage } from './pages/AdminMetricsPage'
import { AdminNodeDetailPage } from './pages/AdminNodeDetailPage'
import { AdminNodesPage } from './pages/AdminNodesPage'
import { AdminPrometheusPage } from './pages/AdminPrometheusPage'
import { AdminUserDetailPage } from './pages/AdminUserDetailPage'
import { AdminUsersPage } from './pages/AdminUsersPage'
import { AdminVPNPage } from './pages/AdminVPNPage'
import { ChangePasswordPage } from './pages/ChangePasswordPage'
import { LoginPage } from './pages/LoginPage'
import { MetricsPage } from './pages/MetricsPage'
import { NodeFormPage } from './pages/NodeFormPage'
import { NodesPage } from './pages/NodesPage'
import { SettingsPage } from './pages/SettingsPage'

export function App() {
  return (
    <AuthProvider>
      <Layout>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route
            path="/change-password"
            element={
              <ProtectedRoute>
                <ChangePasswordPage />
              </ProtectedRoute>
            }
          />
          <Route
            path="/nodes"
            element={
              <NonAdminRoute>
                <NodesPage />
              </NonAdminRoute>
            }
          />
          <Route
            path="/nodes/new"
            element={
              <NonAdminRoute>
                <NodeFormPage />
              </NonAdminRoute>
            }
          />
          <Route
            path="/nodes/:id"
            element={
              <NonAdminRoute>
                <NodeFormPage />
              </NonAdminRoute>
            }
          />
          <Route
            path="/settings"
            element={
              <ProtectedRoute>
                <SettingsPage />
              </ProtectedRoute>
            }
          />
          <Route
            path="/metrics"
            element={
              <NonAdminRoute>
                <MetricsPage />
              </NonAdminRoute>
            }
          />
          <Route
            path="/users"
            element={
              <AdminRoute>
                <AdminUsersPage />
              </AdminRoute>
            }
          />
          <Route
            path="/admin/users/:uuid"
            element={
              <AdminRoute>
                <AdminUserDetailPage />
              </AdminRoute>
            }
          />
          <Route
            path="/admin/nodes"
            element={
              <AdminRoute>
                <AdminNodesPage />
              </AdminRoute>
            }
          />
          <Route
            path="/admin/nodes/:id"
            element={
              <AdminRoute>
                <AdminNodeDetailPage />
              </AdminRoute>
            }
          />
          <Route
            path="/admin/vpn"
            element={
              <AdminRoute>
                <AdminVPNPage />
              </AdminRoute>
            }
          />
          <Route
            path="/admin/prometheus"
            element={
              <AdminRoute>
                <AdminPrometheusPage />
              </AdminRoute>
            }
          />
          <Route
            path="/admin/metrics"
            element={
              <AdminRoute>
                <AdminMetricsPage />
              </AdminRoute>
            }
          />
          <Route path="*" element={<Navigate to="/nodes" replace />} />
        </Routes>
      </Layout>
    </AuthProvider>
  )
}
