package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeBenchmarks is an application.BenchmarkBook that says the close-outs a
// scenario names, or that it could not be read.
type FakeBenchmarks struct {
	Records []application.Benchmark
	Err     error
}

var _ application.BenchmarkBook = (*FakeBenchmarks)(nil)

// Recent implements application.BenchmarkBook.
func (f *FakeBenchmarks) Recent(context.Context) ([]application.Benchmark, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]application.Benchmark(nil), f.Records...), nil
}
