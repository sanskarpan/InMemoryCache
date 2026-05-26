import { Suspense, lazy } from 'react';
import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const Playground = lazy(() => import('./pages/Playground'));
const Visualizer = lazy(() => import('./pages/Visualizer'));
const Benchmarks = lazy(() => import('./pages/Benchmarks'));

function Nav() {
  const links = [
    { to: '/', label: 'Dashboard' },
    { to: '/playground', label: 'Playground' },
    { to: '/visualizer', label: 'Visualizer' },
    { to: '/benchmarks', label: 'Benchmarks' },
  ];
  return (
    <nav className="flex items-center gap-1 px-4 py-3 bg-gray-900 border-b border-gray-800">
      <span className="font-bold text-blue-400 mr-4 text-sm tracking-tight">⚡ Cache Engine</span>
      {links.map((l) => (
        <NavLink
          key={l.to}
          to={l.to}
          end={l.to === '/'}
          className={({ isActive }) =>
            `px-3 py-1.5 rounded text-sm transition-colors ${
              isActive
                ? 'bg-blue-600 text-white'
                : 'text-gray-400 hover:text-white hover:bg-gray-800'
            }`
          }
        >
          {l.label}
        </NavLink>
      ))}
    </nav>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <div className="min-h-screen flex flex-col bg-gray-950 text-gray-100">
        <Nav />
        <main className="flex-1 p-4 max-w-screen-2xl mx-auto w-full">
          <Suspense fallback={<div className="text-sm text-gray-400">Loading…</div>}>
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/playground" element={<Playground />} />
              <Route path="/visualizer" element={<Visualizer />} />
              <Route path="/benchmarks" element={<Benchmarks />} />
            </Routes>
          </Suspense>
        </main>
      </div>
    </BrowserRouter>
  );
}
