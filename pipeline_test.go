package enviv

import (
	"testing"
	"time"
)

// TestReadOrder locks the pipeline: the BMP280 forced-mode write goes first,
// then the SHT40 command, and only then the two reads.
func TestReadOrder(t *testing.T) {
	f := newFake()
	d := New(f)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.ops = nil
	f.mu.Unlock()

	if _, err := d.Read(); err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	ops := append([]byte(nil), f.ops...)
	f.mu.Unlock()

	if len(ops) < 4 {
		t.Fatalf("ops = %v", ops)
	}
	if ops[0] != opBmpCtrl {
		t.Fatalf("first op = %v, want the BMP280 ctrl write", ops)
	}
	if ops[1] != opSHTStart {
		t.Fatalf("second op = %v, want the SHT40 command", ops)
	}
	// The data reads come after both starts.
	if ops[2] != opSHTRead {
		t.Fatalf("third op = %v, want the SHT40 read", ops)
	}
	if ops[3] != opBmpData {
		t.Fatalf("fourth op = %v, want the BMP280 data read", ops)
	}
}

// TestReadTotalTime checks the pipeline against the sequential sum. With x1/x1
// the BMP280 needs ~6.4 ms and the SHT40 10 ms: sequential is ~16.4 ms, the
// pipeline is ~10 ms. The bound of 13 ms separates the two.
func TestReadTotalTime(t *testing.T) {
	f := newFake()
	d := New(f)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	d.SetOversampling(1, 1)

	best := time.Hour
	for i := 0; i < 3; i++ {
		t0 := time.Now()
		if _, err := d.Read(); err != nil {
			t.Fatal(err)
		}
		if dt := time.Since(t0); dt < best {
			best = dt
		}
	}
	if best > 13*time.Millisecond {
		t.Fatalf("Read took %v, want under 13ms (sequential would be ~16.4ms)", best)
	}
}

// TestHeater checks that the heater commands reach the sensor.
func TestHeater(t *testing.T) {
	for _, tc := range []struct {
		p   Precision
		cmd byte
	}{
		{Heater1s, shtMeasureHeat1s},
		{Heater100ms, shtMeasureHeat1},
	} {
		f := newFake()
		d := New(f)
		if err := d.Init(); err != nil {
			t.Fatal(err)
		}
		d.SetPrecision(tc.p)
		if _, err := d.Temperature(); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		cmd := f.shtCmd
		f.mu.Unlock()
		if cmd != tc.cmd {
			t.Fatalf("precision %v: command %#02x, want %#02x", tc.p, cmd, tc.cmd)
		}
	}
}
