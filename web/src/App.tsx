import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { lazy, Suspense } from 'react';
import { Spin } from 'antd';
import MainLayout from './components/MainLayout';
import Login from './pages/Login';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const DhcpServers = lazy(() => import('./pages/DhcpServers'));
const DhcpStatics = lazy(() => import('./pages/DhcpStatics'));
const DhcpLeases = lazy(() => import('./pages/DhcpLeases'));
const DhcpAcl = lazy(() => import('./pages/DhcpAcl'));
const RoutesPage = lazy(() => import('./pages/Routes'));
const RouteTable = lazy(() => import('./pages/RouteTable'));
const SystemPage = lazy(() => import('./pages/System'));
const Backup = lazy(() => import('./pages/Backup'));
const Settings = lazy(() => import('./pages/Settings'));
const About = lazy(() => import('./pages/About'));

const PageFallback = (
  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: 240 }}>
    <Spin tip="加载中…" size="large" />
  </div>
);

function App() {
  return (
    <BrowserRouter>
      <Suspense fallback={PageFallback}>
        <Routes>
          <Route path="/login" element={<Login />} />

          <Route path="/" element={<MainLayout />}>
            <Route index element={<Navigate to="/dashboard" replace />} />
            <Route path="dashboard" element={<Dashboard />} />

            <Route path="dhcp">
              <Route index element={<Navigate to="/dhcp/servers" replace />} />
              <Route path="servers" element={<DhcpServers />} />
              <Route path="statics" element={<DhcpStatics />} />
              <Route path="leases" element={<DhcpLeases />} />
              <Route path="acl" element={<DhcpAcl />} />
            </Route>

            <Route path="routes" element={<RoutesPage />} />
            <Route path="route-table" element={<RouteTable />} />

            <Route path="system" element={<SystemPage />} />
            <Route path="backup" element={<Backup />} />
            <Route path="settings" element={<Settings />} />
            <Route path="about" element={<About />} />
            <Route path="*" element={<Navigate to="/dashboard" replace />} />
          </Route>
        </Routes>
      </Suspense>
    </BrowserRouter>
  );
}

export default App;
