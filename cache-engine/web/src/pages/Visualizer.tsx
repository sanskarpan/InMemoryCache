import { useState, useEffect } from 'react';
import { getSnapshot } from '../api/client';
import type { SnapshotResult } from '../types';
import { LRUViewer } from '../components/visualizer/LRUViewer';
import { LFUViewer } from '../components/visualizer/LFUViewer';
import { ARCViewer } from '../components/visualizer/ARCViewer';

const TABS = ['lru', 'lfu', 'arc'] as const;
type Tab = typeof TABS[number];

function SkeletonCard() {
  return (
    <div className="flex-shrink-0 w-36 rounded-lg border border-gray-800 bg-gray-900 p-3 animate-pulse">
      <div className="h-3 bg-gray-700 rounded mb-2 w-3/4" />
      <div className="h-4 bg-gray-800 rounded w-full" />
    </div>
  );
}

export default function Visualizer() {
  const [tab, setTab] = useState<Tab>('lru');
  const [snap, setSnap] = useState<SnapshotResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    const doFetch = async () => {
      try {
        const s = await getSnapshot(tab);
        if (!cancelled) {
          setSnap(s);
          setError(null);
          setLoading(false);
        }
      } catch {
        if (!cancelled) {
          setError('Failed to load snapshot. Is the backend running?');
          setLoading(false);
        }
      }
    };

    doFetch();
    const id = setInterval(doFetch, 1000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [tab]);

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Cache Visualizer</h1>

      {/* Tabs */}
      <div className="flex gap-2">
        {TABS.map((t) => (
          <button
            key={t}
            onClick={() => { setTab(t); setSnap(null); setLoading(true); setError(null); }}
            className={`px-4 py-2 rounded-lg text-sm font-semibold transition-colors ${
              tab === t
                ? 'bg-blue-600 text-white'
                : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
            }`}
          >
            {t.toUpperCase()}
          </button>
        ))}
      </div>

      <div className="bg-gray-900 rounded-xl border border-gray-800 p-4 min-h-[200px]">
        {error && (
          <div className="text-red-400 text-sm p-8 text-center">
            {error}
          </div>
        )}
        {!error && loading && (
          <div className="flex gap-3 overflow-x-auto py-4 px-2">
            {Array.from({ length: 8 }).map((_, i) => <SkeletonCard key={i} />)}
          </div>
        )}
        {!error && !loading && snap && tab === 'lru' && <LRUViewer entries={snap.entries} />}
        {!error && !loading && snap && tab === 'lfu' && <LFUViewer entries={snap.entries} />}
        {!error && !loading && snap && tab === 'arc' && <ARCViewer snap={snap} />}
        {!error && !loading && snap && snap.entries.length === 0 && (
          <div className="text-gray-500 text-sm p-8 text-center">Cache is empty</div>
        )}
      </div>

      {snap && (
        <div className="flex items-center gap-3 text-xs text-gray-600 font-mono">
          <span>{snap.entries.length} live entries</span>
          {snap.truncated && (
            <span className="text-amber-600">· truncated (showing first 50 per shard)</span>
          )}
          <span>· refreshes every 1s</span>
        </div>
      )}
    </div>
  );
}
