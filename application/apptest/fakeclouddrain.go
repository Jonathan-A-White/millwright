package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeCloudDrainFiles is an application.CloudDrainFiles held in memory: MaxBoxes
// is the max_boxes it says, Kept the old value a drain kept (Drained says one
// did), Writes what each call of SetDrained and SetUndrained was given.
type FakeCloudDrainFiles struct {
	MaxBoxes int
	Kept     int
	Drained  bool
	Writes   []string
}

var _ application.CloudDrainFiles = (*FakeCloudDrainFiles)(nil)

// MaxBoxesKept implements application.CloudDrainFiles.
func (f *FakeCloudDrainFiles) MaxBoxesKept(context.Context) (int, int, bool, error) {
	return f.MaxBoxes, f.Kept, f.Drained, nil
}

// SetDrained implements application.CloudDrainFiles.
func (f *FakeCloudDrainFiles) SetDrained(_ context.Context, old int) error {
	f.Writes = append(f.Writes, "drain "+itoa(old))
	f.MaxBoxes, f.Kept, f.Drained = 0, old, true
	return nil
}

// SetUndrained implements application.CloudDrainFiles.
func (f *FakeCloudDrainFiles) SetUndrained(_ context.Context, back int) error {
	f.Writes = append(f.Writes, "undrain "+itoa(back))
	f.MaxBoxes, f.Kept, f.Drained = back, 0, false
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}
