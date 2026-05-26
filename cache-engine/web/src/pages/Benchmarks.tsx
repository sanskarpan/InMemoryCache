import { useEffect, useRef, useState } from 'react';
import { runBenchmark, getBenchmarkResult } from '../api/client';
import { GroupedBarChart, SingleSeriesBarChart } from '../components/charts/SimpleCharts';
import type { BenchmarkResult } from '../types';

const WORKLOADS = ['zipf', 'sequential', 'uniform', 'temporal', 'scan-resistant', 'write-heavy'];
const POLICIES = ['lru', 'lfu', 'arc'];
const COLORS: Record<string, string> = { lru: '#60a5fa', lfu: '#34d399', arc: '#f472b6' };

const EXPLANATIONS: Record<string, { winner: string; why: string }> = {
  zipf: {
    winner: 'ARC or LFU',
    why: 'Zipf access follows a power-law: a small set of hot keys gets most requests. LFU tracks frequency precisely; ARC adapts its T1/T2 split to favor the hot set. LRU evicts recently-unused hot keys when cold ones push them out.',
  },
  sequential: {
    winner: 'ARC',
    why: 'Sequential scans thrash LRU (every new key evicts the last). ARC shields its T2 (frequently-used) set from T1 scan victims. LFU also resists but is slower to react to access-pattern shifts.',
  },
  uniform: {
    winner: 'All similar',
    why: 'Uniform random access has no locality — every key is equally likely. Hit rate depends purely on cache-size/key-space ratio. All policies perform similarly because frequency and recency give no useful signal.',
  },
  temporal: {
    winner: 'ARC',
    why: 'A hot set of 100 keys shifts every 5,000 ops. ARC adapts p to favor recently-seen keys when the hot set changes, maintaining good hit rate through transitions. LFU is slow to demote the old hot set.',
  },
  'scan-resistant': {
    winner: 'ARC',
    why: '80% hits on a 200-key hot set + 20% cold scan. ARC protects T2 (frequently-used) from T1 scan pollution by limiting T1 to at most p entries. LRU pollutes the cache with cold scan keys.',
  },
  'write-heavy': {
    winner: 'LRU',
    why: '80% writes + 20% reads. Writes always insert at MRU, so LRU keeps the freshest written keys available for reads. LFU and ARC add overhead (frequency counters, ghost lists) without benefit when reads are rare.',
  },
};

interface RunRecord {
  id: string;
  workload: string;
  results: BenchmarkResult[];
}

const PRESETS = [
  { label: 'Zipf: ARC/LFU beat LRU', config: { workload: 'zipf', policies: ['lru','lfu','arc'], cacheSize: 500, durationSec: 5, concurrency: 4 }},
  { label: 'Sequential: LRU thrashing', config: { workload: 'sequential', policies: ['lru','arc'], cacheSize: 500, durationSec: 5, concurrency: 4 }},
  { label: 'Temporal: ARC adapts', config: { workload: 'temporal', policies: ['lru','lfu','arc'], cacheSize: 200, durationSec: 5, concurrency: 4 }},
  { label: 'Scan-resistant: ARC protects hot set', config: { workload: 'scan-resistant', policies: ['lru','lfu','arc'], cacheSize: 200, durationSec: 5, concurrency: 4 }},
];

function HitRateChart({ results }: { results: BenchmarkResult[] }) {
  const data = results.map((r) => ({
    label: r.policy.toUpperCase(),
    value: Math.round(r.hitRate * 1000) / 10,
    color: COLORS[r.policy] ?? '#60a5fa',
  }));
  return (
    <SingleSeriesBarChart data={data} maxValue={100} suffix="%" />
  );
}

function LatencyChart({ results }: { results: BenchmarkResult[] }) {
  const data = results.map((r) => ({
    label: r.policy.toUpperCase(),
    values: [
      { label: 'P50', value: Math.round(r.p50LatencyNs / 1000), color: '#60a5fa' },
      { label: 'P95', value: Math.round(r.p95LatencyNs / 1000), color: '#f59e0b' },
      { label: 'P99', value: Math.round(r.p99LatencyNs / 1000), color: '#ef4444' },
    ],
  }));
  const maxValue = Math.max(1, ...data.flatMap((entry) => entry.values.map((value) => value.value)));
  return (
    <GroupedBarChart data={data} maxValue={maxValue} suffix="µs" />
  );
}

function ExplanationCard({ workload, results }: { workload: string; results: BenchmarkResult[] }) {
  const exp = EXPLANATIONS[workload];
  if (!exp) return null;

  const sorted = [...results].sort((a, b) => b.hitRate - a.hitRate);
  const actualWinner = sorted[0];

  return (
    <div className="bg-gray-900 rounded-xl border border-gray-800 p-4">
      <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wide mb-3">
        Why did {actualWinner?.policy.toUpperCase() ?? exp.winner} win?
      </h2>
      <p className="text-sm text-gray-300 leading-relaxed">{exp.why}</p>
      {sorted.length > 1 && (
        <div className="mt-3 flex gap-3 text-xs font-mono">
          {sorted.map((r, i) => (
            <span key={r.policy} className="flex items-center gap-1">
              <span style={{ color: COLORS[r.policy] }}>{'★'.repeat(3 - i)}</span>
              <span className="text-gray-400">{r.policy.toUpperCase()} {(r.hitRate * 100).toFixed(1)}%</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

export default function Benchmarks() {
  const [workload, setWorkload] = useState('zipf');
  const [policies, setPolicies] = useState(['lru', 'lfu', 'arc']);
  const [cacheSize, setCacheSize] = useState(500);
  const [durationSec, setDurationSec] = useState(5);
  const [concurrency, setConcurrency] = useState(4);
  const [running, setRunning] = useState(false);
  const [results, setResults] = useState<BenchmarkResult[] | null>(null);
  const [currentWorkload, setCurrentWorkload] = useState('zipf');
  const [progress, setProgress] = useState(0);
  const [history, setHistory] = useState<RunRecord[]>([]);
  const [selectedRun, setSelectedRun] = useState<string | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current);
      }
    };
  }, []);

  const togglePolicy = (p: string) => {
    setPolicies((prev) =>
      prev.includes(p) ? prev.filter((x) => x !== p) : [...prev, p]
    );
  };

  const run = async (cfg?: typeof PRESETS[0]['config']) => {
    const config = cfg ?? { workload, policies, cacheSize, durationSec, concurrency };
    if (cfg) {
      setWorkload(config.workload);
      setPolicies(config.policies);
      setCacheSize(config.cacheSize);
      setDurationSec(config.durationSec);
      setConcurrency(config.concurrency);
    }
    setCurrentWorkload(config.workload);
    setRunning(true);
    setResults(null);
    setProgress(0);

    const job = await runBenchmark(config);
    const jobId = job.id;
    let elapsedMs = 0;
    const duration = Math.max(1, config.durationSec) * 1000;

    if (pollRef.current) {
      clearInterval(pollRef.current);
    }
    pollRef.current = setInterval(async () => {
      try {
        elapsedMs += 500;
        setProgress(Math.min(99, (elapsedMs / duration) * 100));

        const found = await getBenchmarkResult(jobId);
        if (found?.status === 'done' && found.results) {
          if (pollRef.current) {
            clearInterval(pollRef.current);
            pollRef.current = null;
          }
          const r = found.results as BenchmarkResult[];
          setResults(r);
          setRunning(false);
          setProgress(100);
          const rec: RunRecord = { id: jobId, workload: config.workload, results: r };
          setHistory((prev) => [rec, ...prev].slice(0, 5));
          setSelectedRun(null);
        }
      } catch (err) {
        console.warn('benchmark polling failed', err);
      }
    }, 500);
  };

  const displayResults = selectedRun
    ? history.find((h) => h.id === selectedRun)?.results ?? results
    : results;
  const displayWorkload = selectedRun
    ? history.find((h) => h.id === selectedRun)?.workload ?? currentWorkload
    : currentWorkload;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Benchmarks</h1>

      {/* Preset buttons */}
      <div className="flex flex-wrap gap-2">
        {PRESETS.map((p) => (
          <button
            key={p.label}
            onClick={() => run(p.config)}
            disabled={running}
            className="px-3 py-1.5 text-xs rounded-lg bg-purple-900/50 text-purple-300 border border-purple-700 hover:bg-purple-900 disabled:opacity-50"
          >
            {p.label}
          </button>
        ))}
      </div>

      {/* Config */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 bg-gray-900 rounded-xl p-4 border border-gray-800">
        <div>
          <label className="text-xs text-gray-400 uppercase block mb-1">Workload</label>
          <select
            className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm"
            value={workload}
            onChange={(e) => setWorkload(e.target.value)}
          >
            {WORKLOADS.map((w) => <option key={w} value={w}>{w}</option>)}
          </select>
        </div>
        <div>
          <label className="text-xs text-gray-400 uppercase block mb-1">Cache Size</label>
          <input
            type="number"
            className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm"
            value={cacheSize}
            onChange={(e) => setCacheSize(Number(e.target.value))}
          />
        </div>
        <div>
          <label className="text-xs text-gray-400 uppercase block mb-1">Duration (sec)</label>
          <select
            className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm"
            value={durationSec}
            onChange={(e) => setDurationSec(Number(e.target.value))}
          >
            {[3, 5, 10, 30].map((d) => <option key={d} value={d}>{d}s</option>)}
          </select>
        </div>
        <div>
          <label className="text-xs text-gray-400 uppercase block mb-1">Concurrency</label>
          <select
            className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-sm"
            value={concurrency}
            onChange={(e) => setConcurrency(Number(e.target.value))}
          >
            {[1, 2, 4, 8].map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
        </div>

        <div className="col-span-2 md:col-span-4">
          <label className="text-xs text-gray-400 uppercase block mb-2">Policies</label>
          <div className="flex gap-3">
            {POLICIES.map((p) => (
              <label key={p} className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={policies.includes(p)}
                  onChange={() => togglePolicy(p)}
                  className="accent-blue-500"
                />
                <span className="text-sm" style={{ color: COLORS[p] }}>{p.toUpperCase()}</span>
              </label>
            ))}
          </div>
        </div>

        <div className="col-span-2 md:col-span-4">
          <button
            onClick={() => run()}
            disabled={running || policies.length === 0}
            className="w-full py-2 rounded-lg bg-blue-600 hover:bg-blue-500 disabled:opacity-50 font-semibold text-sm transition-colors"
          >
            {running ? `Running… ${Math.round(progress)}%` : 'Run Benchmark'}
          </button>
          {running && (
            <div className="mt-2 h-2 bg-gray-800 rounded-full overflow-hidden">
              <div
                className="h-full bg-blue-500 transition-all duration-300"
                style={{ width: `${progress}%` }}
              />
            </div>
          )}
        </div>
      </div>

      {/* Result history */}
      {history.length > 0 && (
        <div className="bg-gray-900 rounded-xl border border-gray-800 p-4">
          <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wide mb-3">
            Run History (last {history.length})
          </h2>
          <div className="flex flex-wrap gap-2">
            <button
              onClick={() => setSelectedRun(null)}
              className={`px-3 py-1.5 text-xs rounded border transition-colors ${
                !selectedRun ? 'bg-blue-800 border-blue-600 text-white' : 'bg-gray-800 border-gray-700 text-gray-400 hover:text-white'
              }`}
            >
              Latest
            </button>
            {history.map((rec) => (
              <button
                key={rec.id}
                onClick={() => setSelectedRun(rec.id)}
                className={`px-3 py-1.5 text-xs rounded border transition-colors ${
                  selectedRun === rec.id ? 'bg-blue-800 border-blue-600 text-white' : 'bg-gray-800 border-gray-700 text-gray-400 hover:text-white'
                }`}
              >
                {rec.workload} · {(Math.max(...rec.results.map(r => r.hitRate)) * 100).toFixed(0)}% peak
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Results */}
      {displayResults && (
        <div className="space-y-6">
          {/* Explanation card */}
          <ExplanationCard workload={displayWorkload} results={displayResults} />

          {/* Hit rate */}
          <div className="bg-gray-900 rounded-xl border border-gray-800 p-4">
            <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wide mb-3">Hit Rate</h2>
            <HitRateChart results={displayResults} />
            <div className="mt-4 grid grid-cols-3 gap-3 text-center">
              {displayResults.map((r) => (
                <div key={r.policy} className="bg-gray-800 rounded-lg p-3">
                  <div className="text-2xl font-bold font-mono" style={{ color: COLORS[r.policy] }}>
                    {(r.hitRate * 100).toFixed(1)}%
                  </div>
                  <div className="text-xs text-gray-500 mt-1">{r.policy.toUpperCase()}</div>
                  <div className="text-xs text-gray-600 mt-1">{(r.opsPerSec / 1000).toFixed(1)}k ops/s</div>
                </div>
              ))}
            </div>
          </div>

          {/* Latency */}
          <div className="bg-gray-900 rounded-xl border border-gray-800 p-4">
            <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wide mb-3">Latency</h2>
            <LatencyChart results={displayResults} />
          </div>

          {/* Evictions */}
          <div className="bg-gray-900 rounded-xl border border-gray-800 p-4">
            <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wide mb-2">Evictions</h2>
            <div className="grid grid-cols-3 gap-3 text-center">
              {displayResults.map((r) => (
                <div key={r.policy} className="bg-gray-800 rounded-lg p-3">
                  <div className="text-xl font-mono font-semibold">{r.evictions.toLocaleString()}</div>
                  <div className="text-xs text-gray-500">{r.policy.toUpperCase()}</div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
