package enviv

import (
	"testing"
	"unsafe"
)

func TestNoAlloc(t *testing.T) {
	f := newFake()
	d := New(f)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(20, func() {
		if _, err := d.Read(); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Temperature(); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Pressure(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("driver path allocates %v per run", allocs)
	}
}

func TestFootprint(t *testing.T) {
	size := unsafe.Sizeof(Device{})
	t.Logf("Device=%d B", size)
	if size > 128 {
		t.Fatalf("Device is %d B, budget is 128 B", size)
	}
}
