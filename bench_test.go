package enviv

import "testing"

func benchDevice(b *testing.B) *Device {
	f := newFake()
	d := New(f)
	if err := d.Init(); err != nil {
		b.Fatal(err)
	}
	return d
}

// BenchmarkTemperature is the cost of one SHT40 measurement, sleeps included.
func BenchmarkTemperature(b *testing.B) {
	d := benchDevice(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Temperature(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPressure is one forced BMP280 measurement, sleeps included.
func BenchmarkPressure(b *testing.B) {
	d := benchDevice(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Pressure(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompensate isolates the integer math of the BMP280.
func BenchmarkCompensate(b *testing.B) {
	c := calib{
		t1: 27504, t2: 26435, t3: -1000,
		p1: 36477, p2: -10685, p3: 3024, p4: 2855, p5: 140,
		p6: -7, p7: 15500, p8: -14600, p9: 6000,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = compensate(519888, 415148, &c)
	}
}

// BenchmarkCRC is the Sensirion CRC of one 2-byte word.
func BenchmarkCRC(b *testing.B) {
	buf := [2]byte{0x66, 0x66}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = crc8(buf[:])
	}
}
