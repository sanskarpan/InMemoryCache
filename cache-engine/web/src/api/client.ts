import ky from 'ky';
import type { GetResponse, SnapshotResult, StatsResponse, BenchmarkJob, CoherenceNodeSnapshot } from '../types';

const apiKey = import.meta.env.VITE_CACHE_ENGINE_API_KEY;

const api = ky.create({
  prefix: '/api',
  timeout: 30000,
  headers: apiKey ? { 'X-API-Key': apiKey } : undefined,
});

let cachedSSEToken: { token: string; expiresAtMs: number } | null = null;

export async function getAuthenticatedSSEURL(path: string): Promise<string> {
  if (!apiKey) {
    return path;
  }
  const now = Date.now();
  if (!cachedSSEToken || cachedSSEToken.expiresAtMs - now < 10_000) {
    const response = await api.post('auth/sse-token').json<{ token: string; expiresAt: string }>();
    cachedSSEToken = {
      token: response.token,
      expiresAtMs: Date.parse(response.expiresAt),
    };
  }
  const separator = path.includes('?') ? '&' : '?';
  return `${path}${separator}access_token=${encodeURIComponent(cachedSSEToken.token)}`;
}

// Cache operations
export async function getKey(store: string, key: string): Promise<GetResponse> {
  return api.get(`cache/${store}/${key}`).json();
}

export async function setKey(store: string, key: string, value: string, ttlMs = 0): Promise<void> {
  await api.put(`cache/${store}/${key}`, {
    json: { value: btoa(value), ttlMs },
  });
}

export async function deleteKey(store: string, key: string): Promise<void> {
  await api.delete(`cache/${store}/${key}`).json();
}

export async function peekKey(store: string, key: string): Promise<GetResponse> {
  return api.get(`cache/${store}/peek/${key}`).json();
}

export async function getStats(store: string): Promise<StatsResponse> {
  return api.get(`cache/${store}/stats`).json();
}

export async function getSnapshot(store: string): Promise<SnapshotResult> {
  return api.get(`cache/${store}/snapshot`).json();
}

export async function purgeStore(store: string): Promise<void> {
  await api.delete(`cache/${store}/purge`).json();
}

export async function configStore(
  store: string,
  opts: { storeLatencyMs?: number; flushNow?: boolean; writePolicy?: string },
): Promise<{ status: string; storeLatencyMs?: number; writePolicy?: string }> {
  return api
    .post(`cache/${store}/config`, {
      json: {
        storeLatencyMs: opts.storeLatencyMs ?? -1,
        flushNow: opts.flushNow ?? false,
        writePolicy: opts.writePolicy,
      },
    })
    .json();
}

// Coherence
export async function coherenceSet(node: string, key: string, value: string): Promise<void> {
  await api.post('coherence/set', { json: { node, key, value: btoa(value) } });
}

export async function getCoherenceNodes(): Promise<Record<string, CoherenceNodeSnapshot>> {
  return api.get('coherence/nodes').json();
}

// Benchmarks
export async function runBenchmark(config: {
  workload: string;
  policies: string[];
  cacheSize: number;
  durationSec: number;
  concurrency: number;
}): Promise<BenchmarkJob> {
  return api.post('benchmark/run', { json: config }).json();
}

export async function getBenchmarkResults(): Promise<BenchmarkJob[]> {
  return api.get('benchmark/results').json();
}

export async function getBenchmarkResult(id: string): Promise<BenchmarkJob> {
  return api.get(`benchmark/results/${id}`).json();
}
