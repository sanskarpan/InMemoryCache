import { create } from 'zustand';
import type { StatsSnapshot, BenchmarkResult, SnapshotResult } from '../types';

interface CacheStore {
  stats: Record<string, StatsSnapshot>;
  snapshots: Record<string, SnapshotResult>;
  benchmarkResults: BenchmarkResult[];
  isRunningBenchmark: boolean;
  setStats: (store: string, s: StatsSnapshot) => void;
  setSnapshot: (store: string, s: SnapshotResult) => void;
  setBenchmarkResults: (r: BenchmarkResult[]) => void;
  setRunningBenchmark: (v: boolean) => void;
}

export const useCacheStore = create<CacheStore>((set) => ({
  stats: {},
  snapshots: {},
  benchmarkResults: [],
  isRunningBenchmark: false,
  setStats: (store, s) => set((state) => ({ stats: { ...state.stats, [store]: s } })),
  setSnapshot: (store, s) => set((state) => ({ snapshots: { ...state.snapshots, [store]: s } })),
  setBenchmarkResults: (r) => set({ benchmarkResults: r }),
  setRunningBenchmark: (v) => set({ isRunningBenchmark: v }),
}));
