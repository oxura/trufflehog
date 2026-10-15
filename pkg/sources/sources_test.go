package sources

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// TestChunkSize tracks the per-chunk metadata footprint. Offset provenance adds
// one function pointer (8 bytes) to the original 104-byte layout; the sparse
// Base64 ranges themselves are allocated only when decoding succeeds.
func TestChunkSize(t *testing.T) {
	t.Parallel()
	assert.Equal(t, unsafe.Sizeof(Chunk{}), uintptr(112), "unexpected Chunk struct size")
}

func TestProgress_UnitProgressKeepsResumeInfo(t *testing.T) {
	t.Parallel()
	var p Progress
	p.SetEncodedResumeInfoFor("unit", "key")
	resumeInfo := p.EncodedResumeInfo

	want := UnitProgress{State: UnitProgressReady, ItemsDone: 1, ItemsTotal: 2, BytesDone: 10, BytesTotal: 20}
	p.SetUnitProgressFor("unit", want)

	got, ok := p.GetUnitProgressFor("unit")
	assert.True(t, ok)
	assert.Equal(t, want, got)
	assert.Equal(t, resumeInfo, p.EncodedResumeInfo)
	assert.Equal(t, "key", p.GetEncodedResumeInfoFor("unit"))

	_, ok = p.GetUnitProgressFor("other unit")
	assert.False(t, ok)
}

func TestUnitProgress_Percent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		progress UnitProgress
		want     int64
	}{
		{name: "nothing reported", progress: UnitProgress{}, want: 0},
		{
			name:     "still counting",
			progress: UnitProgress{State: UnitProgressCounting, BytesDone: 500, BytesTotal: 1000},
			want:     0,
		},
		{
			name:     "count unavailable",
			progress: UnitProgress{State: UnitProgressUnavailable, BytesDone: 500, BytesTotal: 1000},
			want:     0,
		},
		{name: "empty total", progress: UnitProgress{State: UnitProgressReady}, want: 0},
		{
			name:     "partial rounds down",
			progress: UnitProgress{State: UnitProgressReady, BytesDone: 1999, BytesTotal: 3000},
			want:     66,
		},
		{
			name:     "all done caps at 99",
			progress: UnitProgress{State: UnitProgressReady, BytesDone: 1000, BytesTotal: 1000},
			want:     99,
		},
		{
			name:     "done past total caps at 99",
			progress: UnitProgress{State: UnitProgressReady, BytesDone: 1200, BytesTotal: 1000},
			want:     99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.progress.Percent())
		})
	}
}
