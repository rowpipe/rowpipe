package rowpipe

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

// genCSV builds a deterministic n-row CSV in memory for benchmarking.
func genCSV(n int) []byte {
	var b bytes.Buffer
	b.WriteString("id,name,status,amount,note\n")
	statuses := [...]string{"active", "inactive", "pending"}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%d,Customer %d,%s,%d,note for row %d\n",
			i%(n/2+1), i, statuses[i%3], (i*7)%5000, i)
	}
	return b.Bytes()
}

func benchPipe(b *testing.B, data []byte, specs []StageSpec) {
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stages, err := Compile(specs)
		if err != nil {
			b.Fatal(err)
		}
		r, err := NewReader(bytes.NewReader(data), ReaderOptions{})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := Run(r, stages, NewWriter(io.Discard, WriterOptions{})); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFilterSelect is the stateless hot path: it should hold near-constant
// allocations regardless of row count (reused row buffers, no per-row alloc).
func BenchmarkFilterSelect(b *testing.B) {
	data := genCSV(100_000)
	benchPipe(b, data, []StageSpec{
		{Verb: VerbFilter, Arg: "status==active"},
		{Verb: VerbSelect, Arg: "id,name,amount"},
	})
}

// BenchmarkFullPipeline adds dedupe, whose seen-set grows with distinct keys.
func BenchmarkFullPipeline(b *testing.B) {
	data := genCSV(100_000)
	benchPipe(b, data, []StageSpec{
		{Verb: VerbFilter, Arg: "amount>=100"},
		{Verb: VerbDedupe, Arg: "id"},
		{Verb: VerbSelect, Arg: "id,name,amount"},
		{Verb: VerbRename, Arg: "amount=jpy"},
	})
}
