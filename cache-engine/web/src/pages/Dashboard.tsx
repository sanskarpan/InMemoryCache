import { useEffect, useRef, useState } from 'react';
import { useSSE } from '../hooks/useSSE';
import { MultiLineTrendChart, SparkAreaChart } from '../components/charts/SimpleCharts';
import type { StatsSnapshot } from '../types';

const STORES = ['lru', 'lfu', 'arc'];
const COLORS = { lru: '#60a5fa', lfu: '#34d399', arc: '#f472b6' };

const WRITE_POLICY_BADGE: Record<string, string> = {
  'write-through': 'WT',
  'write-back': 'WB',
  'write-around': 'WA',
  none: '—',
};

function StatCard({ store }: { store: string }) {
  const prevEvictions = useRef<number>(0);
  const [evictionHistory, setEvictionHistory] = useState<number[]>(Array(20).fill(0));
  const [opsPerSec, setOpsPerSec] = useState(0);
  const prevOps = useRef<number>(0);
  const lastTick = useRef<number | null>(null);
  const stats = useSSE<StatsSnapshot>(`/api/sse/stats/${store}`, (nextStats) => {
    const now = performance.now();
    const dt = lastTick.current === null ? 0.5 : (now - lastTick.current) / 1000;
    lastTick.current = now;

    const totalOps = nextStats.hits + nextStats.misses;
    const opsDelta = Math.max(0, totalOps - prevOps.current);
    prevOps.current = totalOps;
    setOpsPerSec(dt > 0 ? Math.round(opsDelta / dt) : 0);

    const evDelta = Math.max(0, nextStats.evictions - prevEvictions.current);
    prevEvictions.current = nextStats.evictions;
    setEvictionHistory((prev) => [...prev.slice(1), evDelta]);
  });

  const hitRate = stats ? (stats.hitRate * 100).toFixed(1) : '–';
  const fillPct = stats && stats.capacity > 0 ? (stats.size / stats.capacity) * 100 : 0;
  const color = COLORS[store as keyof typeof COLORS];
  const wpBadge = WRITE_POLICY_BADGE[stats?.writePolicy ?? ''] ?? stats?.writePolicy ?? '–';
  const sparkData = evictionHistory.map((v, i) => ({ i, v }));

  return (
    <div className="bg-gray-900 rounded-xl p-5 border border-gray-800">
      <div className="flex items-center justify-between mb-3">
        <span className="font-semibold text-lg uppercase tracking-wide text-gray-300">{store}</span>
        <div className="flex items-center gap-2">
          {stats && (
            <span className="px-2 py-0.5 rounded text-xs font-mono bg-gray-800 text-gray-400" title="Write policy">
              {wpBadge}
            </span>
          )}
          <span className={`px-2 py-0.5 rounded text-xs font-mono ${
            stats ? 'bg-green-900 text-green-300' : 'bg-gray-800 text-gray-500'
          }`}>
            {stats ? 'LIVE' : 'connecting…'}
          </span>
        </div>
      </div>

      {/* Hit rate gauge */}
      <div className="mb-3">
        <div className="flex items-end justify-between mb-1">
          <span className="text-4xl font-bold font-mono" style={{ color }}>
            {hitRate}%
          </span>
          <span className="text-xs text-gray-500">hit rate</span>
        </div>
        <div className="h-2 bg-gray-800 rounded-full overflow-hidden">
          <div
            className="h-full rounded-full transition-all duration-500"
            style={{ width: `${stats ? stats.hitRate * 100 : 0}%`, background: color }}
          />
        </div>
      </div>

      {/* Capacity fill bar */}
      <div className="mb-3">
        <div className="flex justify-between text-xs text-gray-500 mb-1">
          <span>Capacity</span>
          <span className="font-mono">{stats?.size ?? 0} / {stats?.capacity ?? '–'}</span>
        </div>
        <div className="h-1.5 bg-gray-800 rounded-full overflow-hidden">
          <div
            className="h-full rounded-full transition-all duration-500"
            style={{
              width: `${fillPct}%`,
              background: fillPct > 90 ? '#ef4444' : fillPct > 70 ? '#f59e0b' : color,
            }}
          />
        </div>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-3 gap-2 text-center mb-3">
        <div>
          <div className="text-lg font-mono font-semibold">{stats?.hits ?? 0}</div>
          <div className="text-xs text-gray-500">hits</div>
        </div>
        <div>
          <div className="text-lg font-mono font-semibold">{stats?.misses ?? 0}</div>
          <div className="text-xs text-gray-500">misses</div>
        </div>
        <div>
          <div className="text-lg font-mono font-semibold">{opsPerSec.toLocaleString()}</div>
          <div className="text-xs text-gray-500">ops/s</div>
        </div>
      </div>

      {/* Eviction sparkline */}
      <div>
        <div className="text-xs text-gray-500 mb-1">Evictions (20s)</div>
        <SparkAreaChart values={sparkData.map((point) => point.v)} color={color} />
      </div>
    </div>
  );
}

function MultiLineChart() {
  const [history, setHistory] = useState<Array<{ t: number; lru: number; lfu: number; arc: number }>>([]);
  const [latest, setLatest] = useState<{ lru: number; lfu: number; arc: number }>({ lru: 0, lfu: 0, arc: 0 });

  useSSE<StatsSnapshot>('/api/sse/stats/lru', (stats) => {
    setLatest((prev) => ({ ...prev, lru: stats.hitRate * 100 }));
  });
  useSSE<StatsSnapshot>('/api/sse/stats/lfu', (stats) => {
    setLatest((prev) => ({ ...prev, lfu: stats.hitRate * 100 }));
  });
  useSSE<StatsSnapshot>('/api/sse/stats/arc', (stats) => {
    setLatest((prev) => ({ ...prev, arc: stats.hitRate * 100 }));
  });

  useEffect(() => {
    const id = window.setInterval(() => {
      setHistory((prev) => [
        ...prev.slice(-59),
        {
          t: prev.length === 0 ? 0 : prev[prev.length - 1].t + 1,
          lru: latest.lru,
          lfu: latest.lfu,
          arc: latest.arc,
        },
      ]);
    }, 1000);

    return () => window.clearInterval(id);
  }, [latest]);

  const data = history.map((d, i) => ({ i, ...d }));

  return (
    <div className="bg-gray-900 rounded-xl p-5 border border-gray-800">
      <h2 className="text-sm font-semibold text-gray-400 mb-4 uppercase tracking-wide">
        Hit Rate — 60s Rolling Window
      </h2>
      <MultiLineTrendChart
        series={[
          { key: 'lru', label: 'LRU', color: COLORS.lru, values: data.map((point) => point.lru) },
          { key: 'lfu', label: 'LFU', color: COLORS.lfu, values: data.map((point) => point.lfu) },
          { key: 'arc', label: 'ARC', color: COLORS.arc, values: data.map((point) => point.arc) },
        ]}
      />
    </div>
  );
}

export default function Dashboard() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-gray-100">Cache Dashboard</h1>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {STORES.map((s) => <StatCard key={s} store={s} />)}
      </div>
      <MultiLineChart />
    </div>
  );
}
