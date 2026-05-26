import type { SnapshotResult, CacheEntry } from '../../types';

function ListSection({
  label,
  entries,
  isGhost,
  tooltip,
}: {
  label: string;
  entries: CacheEntry[];
  isGhost: boolean;
  tooltip: string;
}) {
  return (
    <div
      className={`flex-1 min-w-0 rounded-lg border ${
        isGhost ? 'border-dashed border-gray-600 opacity-70' : 'border-gray-700'
      } bg-gray-900 overflow-hidden`}
      title={tooltip}
    >
      <div className={`px-3 py-2 border-b ${isGhost ? 'border-gray-700' : 'border-gray-700'} bg-gray-800`}>
        <span className="font-bold text-sm text-gray-300">{label}</span>
        <span className="ml-2 text-xs text-gray-500">({entries.length})</span>
      </div>
      <div className="p-2 space-y-1 max-h-64 overflow-y-auto">
        {entries.length === 0 && (
          <div className="text-gray-600 text-xs text-center py-4">empty</div>
        )}
        {entries.map((e) => (
          <div
            key={e.key}
            className={`rounded px-2 py-1.5 text-xs font-mono ${
              isGhost
                ? 'bg-gray-800/50 text-gray-500'
                : 'bg-gray-800 text-gray-300'
            }`}
          >
            <div className="truncate">{e.key}</div>
            {!isGhost && e.value && (
              <div className="text-gray-500 truncate text-[10px]">
                {(() => { try { return atob(e.value!); } catch { return '…'; } })()}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

export function ARCViewer({ snap }: { snap: SnapshotResult }) {
  const t1 = snap.entries.filter((e) => e.list === 'T1');
  const t2 = snap.entries.filter((e) => e.list === 'T2');
  const b1 = snap.ghostEntries?.filter((e) => e.list === 'B1') ?? [];
  const b2 = snap.ghostEntries?.filter((e) => e.list === 'B2') ?? [];

  const arcP = snap.arcP ?? 0;
  const capacity = snap.capacity > 0 ? snap.capacity : 1;

  return (
    <div className="space-y-4">
      {/* p indicator */}
      <div className="bg-gray-900 rounded-lg p-3 border border-gray-800">
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm text-gray-400">Adaptive parameter <strong className="text-white">p = {arcP}</strong></span>
          <span className="text-xs text-gray-600">target T1 size</span>
        </div>
        <div className="h-3 bg-gray-800 rounded-full overflow-hidden">
          <div
            className="h-full bg-blue-500 rounded-full transition-all duration-500"
            style={{ width: `${Math.min(100, (arcP / capacity) * 100)}%` }}
          />
        </div>
      </div>

      {/* 4 lists */}
      <div className="grid grid-cols-4 gap-3">
        <ListSection
          label="T1 — recent"
          entries={t1}
          isGhost={false}
          tooltip="Recently used once, in cache"
        />
        <ListSection
          label="B1 — ghost"
          entries={b1}
          isGhost={true}
          tooltip="Recently evicted from T1 (key only, no value)"
        />
        <ListSection
          label="T2 — frequent"
          entries={t2}
          isGhost={false}
          tooltip="Used multiple times, in cache"
        />
        <ListSection
          label="B2 — ghost"
          entries={b2}
          isGhost={true}
          tooltip="Recently evicted from T2 (key only, no value)"
        />
      </div>

      {/* List sizes */}
      {snap.listSizes && (
        <div className="grid grid-cols-4 gap-3 text-center text-xs text-gray-500">
          {Object.entries(snap.listSizes).map(([k, v]) => (
            <div key={k}>{k}: <span className="text-gray-300 font-mono">{v}</span></div>
          ))}
        </div>
      )}
    </div>
  );
}
