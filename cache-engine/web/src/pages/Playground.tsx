import { useState, useRef, useEffect, useCallback } from 'react';
import { getKey, setKey, deleteKey, peekKey, configStore } from '../api/client';
import { useSSE } from '../hooks/useSSE';
import type { GetResponse, StatsSnapshot } from '../types';

type Op = {
  ts: number;
  op: string;
  key: string;
  hit?: boolean;
  latencyUs?: number;
  value?: string;
};

const STORES = ['lru', 'lfu', 'arc', 'lru-sharded'];
const WRITE_POLICIES = ['write-through', 'write-back', 'write-around'] as const;

export default function Playground() {
  const [store, setStore] = useState('lru');
  const [key, setKeyInput] = useState('');
  const [value, setValue] = useState('');
  const [ttl, setTtl] = useState(0);
  const [result, setResult] = useState<GetResponse | null>(null);
  const [log, setLog] = useState<Op[]>([]);
  const [loading, setLoading] = useState(false);
  const [watchMode, setWatchMode] = useState(false);
  const [latencyMs, setLatencyMs] = useState(5);
  const [latencyPending, setLatencyPending] = useState(false);
  const [writePolicyPending, setWritePolicyPending] = useState(false);
  const logRef = useRef<HTMLDivElement>(null);
  const watchRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // SSE stats for write-back dirty count and write policy
  const stats = useSSE<StatsSnapshot>(`/api/sse/stats/${store}`);

  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [log]);

  // Watch mode: poll GET every 200ms
  useEffect(() => {
    if (watchMode && key) {
      watchRef.current = setInterval(async () => {
        try {
          const res = await getKey(store, key);
          setResult(res);
        } catch (err) {
          console.warn('watch mode request failed', err);
        }
      }, 200);
    }
    return () => {
      if (watchRef.current) clearInterval(watchRef.current);
    };
  }, [watchMode, key, store]);

  const addLog = (op: Op) => setLog((prev) => [...prev.slice(-199), op]);

  const exec = useCallback(
    async (opFn: () => Promise<GetResponse | null>, opName: string) => {
      if (!key) return;
      setLoading(true);
      const start = performance.now();
      try {
        const res = await opFn();
        const latencyUs = Math.round((performance.now() - start) * 1000);
        setResult(res);
        addLog({ ts: Date.now(), op: opName, key, hit: res?.hit, latencyUs, value: res?.value });
      } catch (err) {
        console.warn(`${opName} failed`, err);
        addLog({ ts: Date.now(), op: opName, key, latencyUs: 0 });
      } finally {
        setLoading(false);
      }
    },
    [key],
  );

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return;
      if (e.key === 'g') exec(() => getKey(store, key), 'GET');
      if (e.key === 's') exec(async () => { await setKey(store, key, value, ttl); return null; }, 'SET');
      if (e.key === 'd') exec(async () => { await deleteKey(store, key); return null; }, 'DELETE');
      if (e.key === 'p') exec(() => peekKey(store, key), 'PEEK');
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [exec, store, key, value, ttl]);

  const applyLatency = async () => {
    setLatencyPending(true);
    try {
      await configStore(store, { storeLatencyMs: latencyMs });
    } finally {
      setLatencyPending(false);
    }
  };

  const flushNow = async () => {
    await configStore(store, { storeLatencyMs: -1, flushNow: true });
  };

  const applyWritePolicy = async (nextWritePolicy: string) => {
    setWritePolicyPending(true);
    try {
      await configStore(store, { writePolicy: nextWritePolicy });
    } finally {
      setWritePolicyPending(false);
    }
  };

  const isWriteBack = stats?.writePolicy === 'write-back';
  const dirtyCount = stats?.dirtyCount ?? 0;
  const supportsWritePolicy = store !== 'lru-sharded';

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      {/* Left panel */}
      <div className="space-y-4">
        <h1 className="text-2xl font-bold">Playground</h1>

        {/* Store selector */}
        <div>
          <label className="text-xs text-gray-400 uppercase tracking-wide mb-1 block">Store</label>
          <select
            className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm"
            value={store}
            onChange={(e) => { setStore(e.target.value); setResult(null); }}
          >
            {STORES.map((s) => <option key={s} value={s}>{s}</option>)}
          </select>
        </div>

        {/* Inputs */}
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="text-xs text-gray-400 uppercase tracking-wide mb-1 block">Key</label>
            <input
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm font-mono"
              placeholder="my-key"
              value={key}
              onChange={(e) => setKeyInput(e.target.value)}
            />
          </div>
          <div>
            <label className="text-xs text-gray-400 uppercase tracking-wide mb-1 block">TTL (ms, 0=none)</label>
            <input
              type="number"
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm font-mono"
              value={ttl}
              onChange={(e) => setTtl(Number(e.target.value))}
            />
          </div>
        </div>

        <div>
          <label className="text-xs text-gray-400 uppercase tracking-wide mb-1 block">Value</label>
          <input
            className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm font-mono"
            placeholder="my-value"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        </div>

        {/* Buttons */}
        <div className="grid grid-cols-4 gap-2">
          {[
            { label: 'GET (g)', fn: () => exec(() => getKey(store, key), 'GET') },
            { label: 'SET (s)', fn: () => exec(async () => { await setKey(store, key, value, ttl); return null; }, 'SET') },
            { label: 'DEL (d)', fn: () => exec(async () => { await deleteKey(store, key); return null; }, 'DELETE') },
            { label: 'PEEK (p)', fn: () => exec(() => peekKey(store, key), 'PEEK') },
          ].map(({ label, fn }) => (
            <button
              key={label}
              onClick={fn}
              disabled={loading}
              className="py-2 rounded-lg text-sm font-semibold bg-blue-700 hover:bg-blue-600 disabled:opacity-50 transition-colors"
            >
              {label}
            </button>
          ))}
        </div>

        {/* Watch mode toggle */}
        <div className="flex items-center justify-between bg-gray-900 rounded-lg px-4 py-3 border border-gray-800">
          <div>
            <span className="text-sm font-medium">Watch mode</span>
            <p className="text-xs text-gray-500 mt-0.5">Auto-GET every 200ms for this key</p>
          </div>
          <button
            onClick={() => setWatchMode((v) => !v)}
            className={`px-3 py-1.5 rounded text-xs font-semibold transition-colors ${
              watchMode ? 'bg-amber-600 hover:bg-amber-500' : 'bg-gray-700 hover:bg-gray-600'
            }`}
          >
            {watchMode ? 'WATCHING' : 'OFF'}
          </button>
        </div>

        {/* Write policy panel */}
        <div className="bg-gray-900 rounded-xl border border-gray-800 p-4 space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-semibold">Write Policy</span>
            <span className="text-xs font-mono px-2 py-0.5 rounded bg-gray-800 text-blue-300">
              {stats?.writePolicy ?? '–'}
            </span>
          </div>

          {supportsWritePolicy && (
            <div>
              <label className="text-xs text-gray-400 uppercase tracking-wide mb-1 block">Mode</label>
              <select
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm"
                value={stats?.writePolicy ?? 'write-through'}
                onChange={(e) => { void applyWritePolicy(e.target.value); }}
                disabled={writePolicyPending}
              >
                {WRITE_POLICIES.map((policy) => (
                  <option key={policy} value={policy}>{policy}</option>
                ))}
              </select>
            </div>
          )}

          {/* Store latency slider */}
          <div>
            <div className="flex justify-between text-xs text-gray-400 mb-1">
              <span>Store latency</span>
              <span className="font-mono">{latencyMs}ms</span>
            </div>
            <input
              type="range"
              min={0}
              max={100}
              value={latencyMs}
              onChange={(e) => setLatencyMs(Number(e.target.value))}
              className="w-full accent-blue-500"
            />
            <button
              onClick={applyLatency}
              disabled={latencyPending || writePolicyPending}
              className="mt-2 w-full py-1.5 rounded text-xs font-semibold bg-gray-700 hover:bg-gray-600 disabled:opacity-50"
            >
              {latencyPending ? 'Applying…' : 'Apply Latency'}
            </button>
          </div>

          {/* Write-back specific */}
          {isWriteBack && (
            <div className="border-t border-gray-800 pt-3">
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs text-gray-400">Dirty keys</span>
                <span className={`font-mono text-sm font-bold px-2 py-0.5 rounded ${
                  dirtyCount > 0 ? 'bg-amber-900 text-amber-300' : 'bg-gray-800 text-gray-400'
                }`}>
                  {dirtyCount}
                </span>
              </div>
              <button
                onClick={flushNow}
                className="w-full py-1.5 rounded text-xs font-semibold bg-amber-700 hover:bg-amber-600 transition-colors"
              >
                Flush Now
              </button>
            </div>
          )}
        </div>

        {/* Result display */}
        {result && (
          <div className={`rounded-lg p-4 border ${result.hit ? 'bg-green-900/30 border-green-700' : 'bg-red-900/30 border-red-700'}`}>
            <div className="flex items-center gap-2 mb-2">
              <span className={`px-2 py-0.5 rounded text-xs font-bold ${result.hit ? 'bg-green-700' : 'bg-red-700'}`}>
                {result.hit ? 'HIT' : 'MISS'}
              </span>
              <span className="font-mono text-sm text-gray-300">{result.key}</span>
            </div>
            {result.value && (
              <div className="font-mono text-sm text-gray-200 break-all">
                {(() => { try { return atob(result.value!); } catch { return result.value; } })()}
              </div>
            )}
            {result.ttlMs && result.ttlMs > 0 && (
              <div className="mt-2 text-xs text-gray-400">TTL: {result.ttlMs}ms remaining</div>
            )}
          </div>
        )}
      </div>

      {/* Right panel: operation log */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold">Operation Log</h2>
          <button
            onClick={() => setLog([])}
            className="text-xs text-gray-500 hover:text-gray-300"
          >
            Clear
          </button>
        </div>
        <div
          ref={logRef}
          className="h-[500px] overflow-y-auto bg-gray-900 rounded-xl border border-gray-800 p-3 space-y-1 font-mono text-xs"
        >
          {log.length === 0 && (
            <div className="text-gray-600 text-center mt-10">No operations yet — press G/S/D/P</div>
          )}
          {log.map((op, i) => (
            <div key={i} className="flex items-center gap-2 py-0.5">
              <span className="text-gray-600 w-20 flex-shrink-0">
                {new Date(op.ts).toLocaleTimeString()}
              </span>
              <span className={`px-1.5 py-0.5 rounded text-xs font-bold w-14 text-center flex-shrink-0 ${
                op.op === 'GET' || op.op === 'PEEK' ? 'bg-blue-900 text-blue-300' :
                op.op === 'SET' ? 'bg-purple-900 text-purple-300' :
                'bg-red-900 text-red-300'
              }`}>
                {op.op}
              </span>
              <span className="text-gray-300 truncate flex-1">{op.key}</span>
              {op.hit !== undefined && (
                <span className={`px-1 rounded text-xs ${op.hit ? 'text-green-400' : 'text-red-400'}`}>
                  {op.hit ? 'HIT' : 'MISS'}
                </span>
              )}
              {op.latencyUs !== undefined && (
                <span className="text-gray-600 w-16 text-right flex-shrink-0">{op.latencyUs}µs</span>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
