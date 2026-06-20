import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { lazy, Suspense } from 'react';
import { Spin } from 'antd';
import MainLayout from './components/MainLayout';
import Login from './pages/Login';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const NetOverview = lazy(() => import('./pages/NetOverview'));
const NICs = lazy(() => import('./pages/NICs'));
const DhcpServers = lazy(() => import('./pages/DhcpServers'));
const DhcpStatics = lazy(() => import('./pages/DhcpStatics'));
const DhcpLeases = lazy(() => import('./pages/DhcpLeases'));
const DhcpAcl = lazy(() => import('./pages/DhcpAcl'));
const RoutesPage = lazy(() => import('./pages/Routes'));
const PolicyRulesPage = lazy(() => import('./pages/PolicyRules'));
const RouteTable = lazy(() => import('./pages/RouteTable'));
const DnsSettings = lazy(() => import('./pages/dns/DnsSettings'));
const DnsCacheStatus = lazy(() => import('./pages/dns/DnsCacheStatus'));
const DnsRecords = lazy(() => import('./pages/dns/DnsRecords'));
const DnsDomainRoutes = lazy(() => import('./pages/dns/DnsDomainRoutes'));
const Ipv6Settings = lazy(() => import('./pages/ipv6/Ipv6Settings'));
const Ipv6LineDetail = lazy(() => import('./pages/ipv6/Ipv6LineDetail'));
const Ipv6Leases = lazy(() => import('./pages/ipv6/Ipv6Leases'));
const Ipv6PrefixStatic = lazy(() => import('./pages/ipv6/Ipv6PrefixStatic'));
const Ipv6Acl = lazy(() => import('./pages/ipv6/Ipv6Acl'));
const Ipv6Neighbors = lazy(() => import('./pages/ipv6/Ipv6Neighbors'));
const DdnsList = lazy(() => import('./pages/ddns/DdnsList'));
const Speedtest = lazy(() => import('./pages/tools/Speedtest'));
const LogCenter = lazy(() => import('./pages/logcenter/LogCenter'));
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

            <Route path="net" element={<NetOverview />} />
            <Route path="nics" element={<NICs />} />

            <Route path="dhcp">
              <Route index element={<Navigate to="/dhcp/servers" replace />} />
              <Route path="servers" element={<DhcpServers />} />
              <Route path="statics" element={<DhcpStatics />} />
              <Route path="leases" element={<DhcpLeases />} />
              <Route path="acl" element={<DhcpAcl />} />
            </Route>

            <Route path="routes" element={<RoutesPage />} />
            <Route path="policy-rules" element={<PolicyRulesPage />} />
            <Route path="route-table" element={<RouteTable />} />

            <Route path="dns">
              <Route index element={<Navigate to="/dns/settings" replace />} />
              <Route path="settings" element={<DnsSettings />} />
              <Route path="cache" element={<DnsCacheStatus />} />
              <Route path="records" element={<DnsRecords />} />
              <Route path="domain-routes" element={<DnsDomainRoutes />} />
            </Route>

            <Route path="ipv6">
              <Route index element={<Navigate to="/ipv6/settings" replace />} />
              <Route path="settings" element={<Ipv6Settings />} />
              <Route path="line-detail" element={<Ipv6LineDetail />} />
              <Route path="leases" element={<Ipv6Leases />} />
              <Route path="prefix-static" element={<Ipv6PrefixStatic />} />
              <Route path="acl" element={<Ipv6Acl />} />
              <Route path="neighbors" element={<Ipv6Neighbors />} />
            </Route>

            <Route path="ddns" element={<DdnsList />} />
            <Route path="speedtest" element={<Speedtest />} />

            <Route path="logs">
              <Route index element={<Navigate to="/logs/system" replace />} />
              <Route path="system" element={<LogCenter source="system" />} />
              <Route path="operation" element={<LogCenter source="operation" />} />
              <Route path="dhcp" element={<LogCenter source="dhcp" />} />
              <Route path="dialup" element={<LogCenter source="dialup" />} />
              <Route path="ddns" element={<LogCenter source="ddns" />} />
              <Route path="arp" element={<LogCenter source="arp" />} />
            </Route>

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
