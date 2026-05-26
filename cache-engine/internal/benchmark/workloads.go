package benchmark

import "math/rand"

// OpType identifies a cache operation.
type OpType int

const (
	OpGet OpType = iota
	OpSet
)

// Operation is a single cache operation in a workload.
type Operation struct {
	Type  OpType
	Key   string
	Value []byte
}

var (
	readValue  = []byte("v")
	writeValue = []byte("value")
)

func normalizeN(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}

func makeKeys(prefix string, n int) []string {
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = prefix + itoa(i)
	}
	return keys
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// Workload generates a stream of operations.
type Workload interface {
	Name() string
	Next() Operation
	Reset()
}

// --- Sequential ---

type SequentialWorkload struct {
	n       int
	current int
	keys    []string
}

func NewSequential(n int) *SequentialWorkload {
	n = normalizeN(n)
	return &SequentialWorkload{n: n, keys: makeKeys("key:", n)}
}
func (w *SequentialWorkload) Name() string { return "sequential" }
func (w *SequentialWorkload) Reset()       { w.current = 0 }
func (w *SequentialWorkload) Next() Operation {
	key := w.keys[w.current%w.n]
	w.current++
	return Operation{Type: OpGet, Key: key, Value: readValue}
}

// --- Uniform Random ---

type UniformRandomWorkload struct {
	n    int
	rng  *rand.Rand
	keys []string
}

func NewUniformRandom(n int) *UniformRandomWorkload {
	n = normalizeN(n)
	// #nosec G404 -- deterministic pseudo-randomness is intentional for reproducible benchmarks.
	return &UniformRandomWorkload{n: n, rng: rand.New(rand.NewSource(42)), keys: makeKeys("key:", n)}
}
func (w *UniformRandomWorkload) Name() string { return "uniform" }
func (w *UniformRandomWorkload) Reset()       {}
func (w *UniformRandomWorkload) Next() Operation {
	return Operation{Type: OpGet, Key: w.keys[w.rng.Intn(w.n)], Value: readValue}
}

// --- Zipf ---

type ZipfWorkload struct {
	n    int
	zipf *rand.Zipf
	rng  *rand.Rand
	keys []string
}

func NewZipf(n int) *ZipfWorkload {
	n = normalizeN(n)
	// #nosec G404 -- deterministic pseudo-randomness is intentional for reproducible benchmarks.
	rng := rand.New(rand.NewSource(42))
	var maxKey uint64
	if n > 1 {
		maxKey = uint64(n - 1)
	}
	return &ZipfWorkload{
		n:    n,
		rng:  rng,
		zipf: rand.NewZipf(rng, 1.2, 1.0, maxKey),
		keys: makeKeys("key:", n),
	}
}
func (w *ZipfWorkload) Name() string { return "zipf" }
func (w *ZipfWorkload) Reset()       {}
func (w *ZipfWorkload) Next() Operation {
	return Operation{Type: OpGet, Key: w.keys[w.zipf.Uint64()], Value: readValue}
}

// --- Temporal ---

type TemporalWorkload struct {
	n       int
	hotSet  []string
	count   int
	shiftAt int
	rng     *rand.Rand
	keys    []string
}

func NewTemporal(n int) *TemporalWorkload {
	n = normalizeN(n)
	// #nosec G404 -- deterministic pseudo-randomness is intentional for reproducible benchmarks.
	rng := rand.New(rand.NewSource(42))
	w := &TemporalWorkload{n: n, shiftAt: 5000, rng: rng, keys: makeKeys("key:", n)}
	w.generateHotSet()
	return w
}

func (w *TemporalWorkload) generateHotSet() {
	w.hotSet = make([]string, 100)
	for i := range w.hotSet {
		w.hotSet[i] = w.keys[w.rng.Intn(w.n)]
	}
}

func (w *TemporalWorkload) Name() string { return "temporal" }
func (w *TemporalWorkload) Reset()       { w.count = 0 }
func (w *TemporalWorkload) Next() Operation {
	w.count++
	if w.count%w.shiftAt == 0 {
		w.generateHotSet()
	}
	return Operation{Type: OpGet, Key: w.hotSet[w.rng.Intn(len(w.hotSet))], Value: readValue}
}

// --- Scan Resistant (80% hot + 20% sequential scan) ---

type ScanResistantWorkload struct {
	hotN     int
	scanN    int
	scanPos  int
	rng      *rand.Rand
	hotKeys  []string
	scanKeys []string
}

func NewScanResistant(hotN, scanN int) *ScanResistantWorkload {
	hotN = normalizeN(hotN)
	scanN = normalizeN(scanN)
	// #nosec G404 -- deterministic pseudo-randomness is intentional for reproducible benchmarks.
	return &ScanResistantWorkload{
		hotN: hotN, scanN: scanN, rng: rand.New(rand.NewSource(42)),
		hotKeys: makeKeys("hot:", hotN), scanKeys: makeKeys("scan:", scanN),
	}
}
func (w *ScanResistantWorkload) Name() string { return "scan-resistant" }
func (w *ScanResistantWorkload) Reset()       { w.scanPos = 0 }
func (w *ScanResistantWorkload) Next() Operation {
	if w.rng.Float64() < 0.8 {
		return Operation{Type: OpGet, Key: w.hotKeys[w.rng.Intn(w.hotN)], Value: readValue}
	}
	key := w.scanKeys[w.scanPos%w.scanN]
	w.scanPos++
	return Operation{Type: OpGet, Key: key, Value: readValue}
}

// --- Write Heavy ---

type WriteHeavyWorkload struct {
	n    int
	rng  *rand.Rand
	keys []string
}

func NewWriteHeavy(n int) *WriteHeavyWorkload {
	n = normalizeN(n)
	// #nosec G404 -- deterministic pseudo-randomness is intentional for reproducible benchmarks.
	return &WriteHeavyWorkload{n: n, rng: rand.New(rand.NewSource(42)), keys: makeKeys("key:", n)}
}
func (w *WriteHeavyWorkload) Name() string { return "write-heavy" }
func (w *WriteHeavyWorkload) Reset()       {}
func (w *WriteHeavyWorkload) Next() Operation {
	key := w.keys[w.rng.Intn(w.n)]
	if w.rng.Float64() < 0.8 {
		return Operation{Type: OpSet, Key: key, Value: writeValue}
	}
	return Operation{Type: OpGet, Key: key, Value: readValue}
}
