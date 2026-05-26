export interface CacheEntry {
  key: string;
  value?: string; // base64
  expiresAt?: string;
  ttlMs?: number;
  freq: number;
  createdAt?: string;
  hitCount?: number;
  sizeBytes?: number;
  list?: string; // T1/T2/B1/B2 for ARC
  position: number;
}

export interface SnapshotResult {
  policy: string;
  entries: CacheEntry[];
  ghostEntries?: CacheEntry[];
  arcP?: number;
  listSizes?: Record<string, number>;
  capacity: number;
  truncated?: boolean;
}

export interface StatsSnapshot {
  hits: number;
  misses: number;
  sets: number;
  deletes: number;
  evictions: number;
  ttlExpiries: number;
  bytesStored: number;
  writeStoreOps: number;
  readStoreOps: number;
  hitRate: number;
  missRate: number;
  evictionRate: number;
  timestamp: string;
  // Enriched fields added by SSE endpoint
  size: number;
  capacity: number;
  writePolicy: string;
  dirtyCount?: number;
}

export interface StatsResponse {
  policy: string;
  writePolicy: string;
  capacity: number;
  size: number;
  hitRate: number;
  hits: number;
  misses: number;
  evictions: number;
  ttlExpiries: number;
  bytesStored: number;
  writeStoreOps: number;
  readStoreOps: number;
  dirtyCount?: number;
  storeLatencyMs?: number;
}

export interface GetResponse {
  key: string;
  value?: string; // base64
  hit: boolean;
  ttlMs?: number;
  freq?: number;
}

export interface BenchmarkResult {
  policy: string;
  workload: string;
  hitRate: number;
  opsPerSec: number;
  avgLatencyNs: number;
  p50LatencyNs: number;
  p95LatencyNs: number;
  p99LatencyNs: number;
  evictions: number;
  totalOps: number;
}

export interface BenchmarkJob {
  id: string;
  status: 'running' | 'done' | 'error';
  error?: string;
  results?: BenchmarkResult[];
  startedAt: string;
}

export interface CoherenceNodeSnapshot {
  nodeId: string;
  entries: CacheEntry[];
  len: number;
}

export type PolicyType = 'lru' | 'lfu' | 'arc' | 'lru-sharded';
export type WorkloadType = 'sequential' | 'uniform' | 'zipf' | 'temporal' | 'scan-resistant' | 'write-heavy';
