import { beforeEach, describe, expect, it } from 'vitest';
import { useCacheStore } from './cacheStore';

describe('useCacheStore', () => {
  beforeEach(() => {
    useCacheStore.setState({
      stats: {},
      snapshots: {},
      benchmarkResults: [],
      isRunningBenchmark: false,
    });
  });

  it('updates per-store stats without dropping existing entries', () => {
    useCacheStore.getState().setStats('lru', {
      hits: 1,
      misses: 1,
      sets: 1,
      deletes: 0,
      evictions: 0,
      ttlExpiries: 0,
      bytesStored: 4,
      writeStoreOps: 0,
      readStoreOps: 0,
      hitRate: 0.5,
      missRate: 0.5,
      evictionRate: 0,
      timestamp: new Date().toISOString(),
      size: 1,
      capacity: 10,
      writePolicy: 'write-through',
    });
    useCacheStore.getState().setStats('lfu', {
      hits: 2,
      misses: 0,
      sets: 2,
      deletes: 0,
      evictions: 0,
      ttlExpiries: 0,
      bytesStored: 8,
      writeStoreOps: 0,
      readStoreOps: 0,
      hitRate: 1,
      missRate: 0,
      evictionRate: 0,
      timestamp: new Date().toISOString(),
      size: 2,
      capacity: 10,
      writePolicy: 'write-through',
    });

    const state = useCacheStore.getState();
    expect(state.stats.lru.size).toBe(1);
    expect(state.stats.lfu.size).toBe(2);
  });

  it('tracks benchmark state transitions', () => {
    useCacheStore.getState().setRunningBenchmark(true);
    useCacheStore.getState().setBenchmarkResults([
      {
        policy: 'lru',
        workload: 'zipf',
        hitRate: 0.9,
        opsPerSec: 1234,
        avgLatencyNs: 10,
        p50LatencyNs: 8,
        p95LatencyNs: 20,
        p99LatencyNs: 30,
        evictions: 2,
        totalOps: 100,
      },
    ]);
    useCacheStore.getState().setRunningBenchmark(false);

    const state = useCacheStore.getState();
    expect(state.isRunningBenchmark).toBe(false);
    expect(state.benchmarkResults).toHaveLength(1);
    expect(state.benchmarkResults[0].policy).toBe('lru');
  });
});
