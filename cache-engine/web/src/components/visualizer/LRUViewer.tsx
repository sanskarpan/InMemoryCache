import { AnimatePresence, motion } from 'framer-motion';
import { useState, useEffect } from 'react';
import type { CacheEntry } from '../../types';

function TTLBar({ ttlMs }: { ttlMs: number }) {
  const [remaining, setRemaining] = useState(ttlMs);
  const [initial] = useState(ttlMs);

  useEffect(() => {
    const start = Date.now();
    const interval = setInterval(() => {
      setRemaining(Math.max(0, ttlMs - (Date.now() - start)));
    }, 100);
    return () => clearInterval(interval);
  }, [ttlMs]);

  const pct = initial > 0 ? (remaining / initial) * 100 : 0;
  return (
    <div className="mt-2 h-1 bg-gray-700 rounded">
      <div
        className="h-1 rounded transition-all"
        style={{
          width: `${pct}%`,
          background: pct > 50 ? '#22c55e' : pct > 20 ? '#f59e0b' : '#ef4444',
        }}
      />
    </div>
  );
}

export function LRUViewer({ entries }: { entries: CacheEntry[] }) {
  return (
    <div className="flex gap-3 overflow-x-auto py-4 px-2">
      <AnimatePresence initial={false}>
        {entries.slice(0, 20).map((entry, i) => (
          <motion.div
            key={entry.key}
            layoutId={entry.key}
            initial={{ opacity: 0, x: 60 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: -60 }}
            transition={{ type: 'spring', stiffness: 300, damping: 30 }}
            className={`flex-shrink-0 w-36 rounded-lg border p-3 ${
              i === 0
                ? 'border-green-500 bg-green-900/30'
                : 'border-gray-700 bg-gray-800'
            }`}
          >
            <div className="font-mono text-xs text-gray-400 truncate">{entry.key}</div>
            <div className="text-sm font-medium truncate mt-1 text-gray-200">
              {entry.value ? (() => { try { return atob(entry.value!); } catch { return entry.value; } })() : '–'}
            </div>
            {(entry.ttlMs ?? 0) > 0 && <TTLBar ttlMs={entry.ttlMs!} />}
          </motion.div>
        ))}
      </AnimatePresence>
      {entries.length > 20 && (
        <div className="flex-shrink-0 w-24 flex items-center justify-center text-gray-500 text-sm">
          +{entries.length - 20} more
        </div>
      )}
    </div>
  );
}
