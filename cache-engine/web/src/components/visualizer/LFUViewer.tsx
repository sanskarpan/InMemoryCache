import { AnimatePresence, motion } from 'framer-motion';
import type { CacheEntry } from '../../types';

export function LFUViewer({ entries }: { entries: CacheEntry[] }) {
  // Group by frequency
  const groups: Record<number, CacheEntry[]> = {};
  for (const e of entries) {
    const f = e.freq || 1;
    if (!groups[f]) groups[f] = [];
    groups[f].push(e);
  }
  const freqs = Object.keys(groups).map(Number).sort((a, b) => a - b);
  const minFreq = freqs[0] ?? 1;

  if (freqs.length === 0) {
    return <div className="text-gray-500 text-sm p-4">Cache is empty</div>;
  }

  return (
    <div className="flex gap-4 overflow-x-auto pb-4 pt-2">
      {freqs.map((freq) => (
        <div
          key={freq}
          className={`flex-shrink-0 w-36 rounded-lg border ${
            freq === minFreq ? 'border-amber-500' : 'border-gray-700'
          } bg-gray-900`}
        >
          <div className={`text-xs font-bold px-3 py-2 border-b ${
            freq === minFreq ? 'border-amber-500 text-amber-400' : 'border-gray-700 text-gray-400'
          }`}>
            f={freq}
            <span className="ml-2 text-gray-600">({groups[freq].length})</span>
          </div>
          <div className="p-2 space-y-1 max-h-80 overflow-y-auto">
            <AnimatePresence>
              {groups[freq].map((entry) => {
                const saturation = Math.min(freq / 10, 1);
                return (
                  <motion.div
                    key={entry.key}
                    layoutId={entry.key}
                    layout
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ type: 'spring', stiffness: 200, damping: 25 }}
                    className="rounded px-2 py-1.5 text-xs font-mono truncate"
                    style={{
                      background: `rgba(59, 130, 246, ${0.1 + saturation * 0.4})`,
                      border: `1px solid rgba(59, 130, 246, ${0.2 + saturation * 0.5})`,
                    }}
                  >
                    <div className="text-gray-300 truncate">{entry.key}</div>
                    <div className="text-gray-500 truncate text-[10px]">
                      {entry.value ? (() => { try { return atob(entry.value!); } catch { return '…'; } })() : '–'}
                    </div>
                  </motion.div>
                );
              })}
            </AnimatePresence>
          </div>
        </div>
      ))}
    </div>
  );
}
