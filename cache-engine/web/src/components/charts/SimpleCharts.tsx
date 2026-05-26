type Series = {
  key: string;
  label: string;
  color: string;
  values: number[];
};

type BarDatum = {
  label: string;
  value: number;
  color: string;
};

type GroupedBarDatum = {
  label: string;
  values: Array<{
    label: string;
    value: number;
    color: string;
  }>;
};

const CHART_WIDTH = 640;
const CHART_HEIGHT = 220;
const MARGIN = { top: 16, right: 16, bottom: 32, left: 44 };

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function safeMax(value: number): number {
  return value > 0 ? value : 1;
}

function buildLinePath(values: number[], width: number, height: number, maxValue: number): string {
  if (values.length === 0) {
    return '';
  }
  const boundedMax = safeMax(maxValue);
  if (values.length === 1) {
    const y = height - (values[0] / boundedMax) * height;
    return `M 0 ${y} L ${width} ${y}`;
  }
  return values
    .map((value, index) => {
      const x = (index / (values.length - 1)) * width;
      const y = height - (value / boundedMax) * height;
      return `${index === 0 ? 'M' : 'L'} ${x} ${y}`;
    })
    .join(' ');
}

function buildAreaPath(values: number[], width: number, height: number, maxValue: number): string {
  const linePath = buildLinePath(values, width, height, maxValue);
  if (!linePath) {
    return '';
  }
  return `${linePath} L ${width} ${height} L 0 ${height} Z`;
}

function axisTicks(maxValue: number, steps: number): number[] {
  if (maxValue <= 0) {
    return Array.from({ length: steps + 1 }, (_, index) => index);
  }
  return Array.from({ length: steps + 1 }, (_, index) => (maxValue / steps) * index);
}

export function SparkAreaChart({
  values,
  color,
  height = 40,
}: {
  values: number[];
  color: string;
  height?: number;
}) {
  const width = 240;
  const maxValue = Math.max(1, ...values);
  const areaPath = buildAreaPath(values, width, height, maxValue);
  const linePath = buildLinePath(values, width, height, maxValue);

  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full" role="img" aria-label="Sparkline chart">
      <path d={areaPath} fill={color} fillOpacity="0.16" />
      <path d={linePath} fill="none" stroke={color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

export function MultiLineTrendChart({
  series,
  maxValue = 100,
}: {
  series: Series[];
  maxValue?: number;
}) {
  const boundedMax = safeMax(maxValue);
  const plotWidth = CHART_WIDTH - MARGIN.left - MARGIN.right;
  const plotHeight = CHART_HEIGHT - MARGIN.top - MARGIN.bottom;
  const ticks = axisTicks(boundedMax, 4);

  return (
    <div className="space-y-3">
      <svg viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`} className="w-full" role="img" aria-label="Trend chart">
        <g transform={`translate(${MARGIN.left},${MARGIN.top})`}>
          {ticks.map((tick) => {
            const y = plotHeight - (tick / boundedMax) * plotHeight;
            return (
              <g key={tick}>
                <line x1={0} y1={y} x2={plotWidth} y2={y} stroke="#374151" strokeDasharray="3 3" />
                <text x={-10} y={y + 4} fill="#9ca3af" fontSize="11" textAnchor="end">
                  {Math.round(tick)}%
                </text>
              </g>
            );
          })}
          {series.map((entry) => (
            <path
              key={entry.key}
              d={buildLinePath(entry.values, plotWidth, plotHeight, boundedMax)}
              fill="none"
              stroke={entry.color}
              strokeWidth="2.5"
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          ))}
        </g>
      </svg>
      <div className="flex flex-wrap gap-4 text-xs text-gray-400">
        {series.map((entry) => (
          <span key={entry.key} className="flex items-center gap-2">
            <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: entry.color }} />
            {entry.label}
          </span>
        ))}
      </div>
    </div>
  );
}

export function SingleSeriesBarChart({
  data,
  maxValue,
  suffix = '',
}: {
  data: BarDatum[];
  maxValue: number;
  suffix?: string;
}) {
  const boundedMax = safeMax(maxValue);
  const plotWidth = CHART_WIDTH - MARGIN.left - MARGIN.right;
  const plotHeight = CHART_HEIGHT - MARGIN.top - MARGIN.bottom;
  const gap = 20;
  const barWidth = data.length > 0 ? (plotWidth - gap * Math.max(0, data.length - 1)) / data.length : plotWidth;
  const ticks = axisTicks(boundedMax, 4);

  return (
    <svg viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`} className="w-full" role="img" aria-label="Bar chart">
      <g transform={`translate(${MARGIN.left},${MARGIN.top})`}>
        {ticks.map((tick) => {
          const y = plotHeight - (tick / boundedMax) * plotHeight;
          return (
            <g key={tick}>
              <line x1={0} y1={y} x2={plotWidth} y2={y} stroke="#374151" strokeDasharray="3 3" />
              <text x={-10} y={y + 4} fill="#9ca3af" fontSize="11" textAnchor="end">
                {Math.round(tick)}{suffix}
              </text>
            </g>
          );
        })}
        {data.map((datum, index) => {
          const x = index * (barWidth + gap);
          const height = clamp((datum.value / boundedMax) * plotHeight, 0, plotHeight);
          const y = plotHeight - height;
          return (
            <g key={datum.label}>
              <title>{`${datum.label}: ${datum.value}${suffix}`}</title>
              <rect x={x} y={y} width={barWidth} height={height} rx="8" fill={datum.color} />
              <text x={x + barWidth / 2} y={plotHeight + 18} fill="#9ca3af" fontSize="11" textAnchor="middle">
                {datum.label}
              </text>
            </g>
          );
        })}
      </g>
    </svg>
  );
}

export function GroupedBarChart({
  data,
  maxValue,
  suffix = '',
}: {
  data: GroupedBarDatum[];
  maxValue: number;
  suffix?: string;
}) {
  const boundedMax = safeMax(maxValue);
  const plotWidth = CHART_WIDTH - MARGIN.left - MARGIN.right;
  const plotHeight = CHART_HEIGHT - MARGIN.top - MARGIN.bottom;
  const groupGap = 28;
  const barGap = 8;
  const groupWidth = data.length > 0 ? (plotWidth - groupGap * Math.max(0, data.length - 1)) / data.length : plotWidth;
  const barsPerGroup = Math.max(1, ...data.map((datum) => datum.values.length));
  const barWidth = (groupWidth - barGap * Math.max(0, barsPerGroup - 1)) / barsPerGroup;
  const ticks = axisTicks(boundedMax, 4);
  const legend = data[0]?.values ?? [];

  return (
    <div className="space-y-3">
      <svg viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`} className="w-full" role="img" aria-label="Grouped bar chart">
        <g transform={`translate(${MARGIN.left},${MARGIN.top})`}>
        {ticks.map((tick) => {
          const y = plotHeight - (tick / boundedMax) * plotHeight;
          return (
            <g key={tick}>
              <line x1={0} y1={y} x2={plotWidth} y2={y} stroke="#374151" strokeDasharray="3 3" />
                <text x={-10} y={y + 4} fill="#9ca3af" fontSize="11" textAnchor="end">
                  {Math.round(tick)}{suffix}
                </text>
              </g>
            );
          })}
          {data.map((datum, groupIndex) => {
            const groupX = groupIndex * (groupWidth + groupGap);
            return (
              <g key={datum.label}>
                {datum.values.map((item, itemIndex) => {
                  const x = groupX + itemIndex * (barWidth + barGap);
                  const height = clamp((item.value / boundedMax) * plotHeight, 0, plotHeight);
                  const y = plotHeight - height;
                  return (
                    <g key={item.label}>
                      <title>{`${datum.label} ${item.label}: ${item.value}${suffix}`}</title>
                      <rect x={x} y={y} width={barWidth} height={height} rx="6" fill={item.color} />
                    </g>
                  );
                })}
                <text x={groupX + groupWidth / 2} y={plotHeight + 18} fill="#9ca3af" fontSize="11" textAnchor="middle">
                  {datum.label}
                </text>
              </g>
            );
          })}
        </g>
      </svg>
      <div className="flex flex-wrap gap-4 text-xs text-gray-400">
        {legend.map((item) => (
          <span key={item.label} className="flex items-center gap-2">
            <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: item.color }} />
            {item.label}
          </span>
        ))}
      </div>
    </div>
  );
}
