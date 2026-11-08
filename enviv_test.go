package enviv

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"
)

var errNack = errors.New("fake: nack")

// fakeBus emulates the SHT40 and the BMP280 of one unit.
type fakeBus struct {
	mu sync.Mutex

	// SHT40
	shtAddr  uint16
	shtReads int
	shtCmd   byte
	shtResp  [6]byte // temperature and humidity words with CRC
	shtDead  bool
	shtSN    [6]byte

	// BMP280
	bmpAddr  uint16
	bmpID    byte
	bmpCal   [24]byte
	bmpData  [6]byte
	bmpDead  bool
	regs     map[byte]byte
	forcedIn int
	ops      []byte
}

// Op markers recorded by the fake, to check the order of a pipelined read.
const (
	opBmpCtrl  byte = 1
	opBmpData  byte = 2
	opSHTStart byte = 3
	opSHTRead  byte = 4
)

// makeSHT builds a response for raw temperature and humidity with valid CRCs.
func makeSHT(rawT, rawRH uint16) [6]byte {
	var b [6]byte
	binary.BigEndian.PutUint16(b[0:2], rawT)
	b[2] = crc8(b[0:2])
	binary.BigEndian.PutUint16(b[3:5], rawRH)
	b[5] = crc8(b[3:5])
	return b
}

// makeCalib builds the 24-byte calibration block of the reference example.
func makeCalib() [24]byte {
	vals := []int32{27504, 26435, -1000, 36477, -10685, 3024, 2855, 140, -7, 15500, -14600, 6000}
	var b [24]byte
	for i, v := range vals {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(int16(v)))
	}
	return b
}

func newFake() *fakeBus {
	f := &fakeBus{
		shtAddr: SHT40Address,
		bmpAddr: BMP280Address,
		bmpID:   bmpChipID,
		bmpCal:  makeCalib(),
		regs:    map[byte]byte{},
	}
	// 25.08 °C and ~50 %RH, the values the SHT40 would report.
	f.shtResp = makeSHT(26214, 32768)
	// temperature 2508 (0.01 °C) and pressure 100653 Pa: the Bosch example.
	rawT, rawP := uint32(519888), uint32(415148)
	f.bmpData[0] = byte(rawP >> 12)
	f.bmpData[1] = byte(rawP >> 4)
	f.bmpData[2] = byte(rawP << 4)
	f.bmpData[3] = byte(rawT >> 12)
	f.bmpData[4] = byte(rawT >> 4)
	f.bmpData[5] = byte(rawT << 4)
	return f
}

func (f *fakeBus) Tx(addr uint16, w, r []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch addr {
	case f.shtAddr:
		if f.shtDead {
			return errNack
		}
		if len(w) > 0 {
			f.shtCmd = w[0]
			if w[0] != shtSoftReset && w[0] != shtSerial {
				f.ops = append(f.ops, opSHTStart)
			}
			if f.shtCmd == shtSerial {
				copy(f.shtSN[:], []byte{0x12, 0x34, 0, 0x56, 0x78, 0})
				f.shtSN[2] = crc8(f.shtSN[0:2])
				f.shtSN[5] = crc8(f.shtSN[3:5])
			}
			return nil
		}
		if len(r) > 0 {
			f.shtReads++
			if f.shtCmd != shtSerial {
				f.ops = append(f.ops, opSHTRead)
			}
			if f.shtCmd == shtSerial {
				copy(r, f.shtSN[:])
				return nil
			}
			copy(r, f.shtResp[:])
			return nil
		}
		return nil

	case f.bmpAddr:
		if f.bmpDead {
			return errNack
		}
		if len(w) == 1 {
			reg := w[0]
			switch reg {
			case bmpChipIDReg:
				if len(r) > 0 {
					r[0] = f.bmpID
					return nil
				}
			case bmpCalibReg:
				if len(r) >= bmpCalibLen {
					copy(r, f.bmpCal[:])
					return nil
				}
			case bmpDataReg:
				if len(r) >= bmpDataLen {
					f.ops = append(f.ops, opBmpData)
					copy(r, f.bmpData[:])
					return nil
				}
			}
			if len(r) > 0 {
				r[0] = f.regs[reg]
			}
			return nil
		}
		if len(w) == 2 {
			f.regs[w[0]] = w[1]
			if w[0] == bmpCtlReg && w[1]&0b11 == bmpModeForced {
				f.forcedIn++
				f.ops = append(f.ops, opBmpCtrl)
			}
			return nil
		}
		return nil
	}
	return errNack
}

func newDeviceT(t *testing.T) (*Device, *fakeBus) {
	t.Helper()
	f := newFake()
	d := New(f)
	if err := d.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return d, f
}

func TestInit(t *testing.T) {
	d, _ := newDeviceT(t)
	if !d.cal.loaded {
		t.Fatal("calibration not loaded")
	}
	if d.cal.t1 != 27504 || d.cal.p6 != -7 || d.cal.p9 != 6000 {
		t.Fatalf("calibration = %+v", d.cal)
	}

	// Wrong chip id.
	f2 := newFake()
	f2.bmpID = 0x60
	if err := New(f2).Init(); err != ErrBadModel {
		t.Fatalf("bad id = %v", err)
	}

	// Dead sensors.
	f3 := newFake()
	f3.shtDead = true
	if err := New(f3).Init(); err != ErrNotConnected {
		t.Fatalf("dead sht = %v", err)
	}
	f4 := newFake()
	f4.bmpDead = true
	if err := New(f4).Init(); err != ErrNotConnected {
		t.Fatalf("dead bmp = %v", err)
	}

	// No bus at all.
	if err := New(nil).Init(); err != ErrNotConnected {
		t.Fatalf("nil bus = %v", err)
	}
}

func TestRead(t *testing.T) {
	d, _ := newDeviceT(t)
	s, err := d.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// SHT40 formula with raw 26214 and 32768.
	if s.Temperature != 25000 {
		t.Fatalf("temperature = %d, want 25000", s.Temperature)
	}
	if s.Humidity != 56500 {
		t.Fatalf("humidity = %d, want 56500", s.Humidity)
	}
	// BMP280 reference: 25.08 °C and 100653 Pa.
	if s.Pressure != 100653 {
		t.Fatalf("pressure = %d, want 100653", s.Pressure)
	}
	if got := s.Celsius(); got < 24.9 || got > 25.1 {
		t.Fatalf("Celsius = %v", got)
	}
	if got := s.HPa(); got < 1006 || got > 1007 {
		t.Fatalf("HPa = %v", got)
	}
}

func TestSingleReads(t *testing.T) {
	d, _ := newDeviceT(t)
	tp, err := d.Temperature()
	if err != nil || tp != 25000 {
		t.Fatalf("Temperature = %d, %v", tp, err)
	}
	rh, err := d.Humidity()
	if err != nil || rh != 56500 {
		t.Fatalf("Humidity = %d, %v", rh, err)
	}
	p, err := d.Pressure()
	if err != nil || p != 100653 {
		t.Fatalf("Pressure = %d, %v", p, err)
	}
}

func TestCRC(t *testing.T) {
	f := newFake()
	f.shtResp[2] ^= 0xFF // break the temperature CRC
	d := New(f)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read(); err != ErrCRC {
		t.Fatalf("bad crc = %v", err)
	}
}

func TestSerial(t *testing.T) {
	d, _ := newDeviceT(t)
	sn, err := d.SerialNumber()
	if err != nil {
		t.Fatalf("SerialNumber: %v", err)
	}
	if sn != 0x12345678 {
		t.Fatalf("serial = %#08x", sn)
	}
}

func TestPrecision(t *testing.T) {
	d, f := newDeviceT(t)

	// The command of the precision reaches the sensor.
	d.SetPrecision(Low)
	if _, err := d.Temperature(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	cmd := f.shtCmd
	f.mu.Unlock()
	if cmd != shtMeasureLow {
		t.Fatalf("command = %#02x, want %#02x", cmd, shtMeasureLow)
	}
}

func TestOversampling(t *testing.T) {
	d, f := newDeviceT(t)
	d.SetOversampling(1, 1) // x1 and x1: 1 << 0
	if _, err := d.Pressure(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	ctrl := f.regs[bmpCtlReg]
	f.mu.Unlock()
	if want := byte(1<<5 | 1<<2 | bmpModeForced); ctrl != want {
		t.Fatalf("ctrl = %#02x, want %#02x", ctrl, want)
	}
	// Clamping.
	d.SetOversampling(9, 9)
	if d.osT != 5 || d.osP != 5 {
		t.Fatalf("clamp = %d %d", d.osT, d.osP)
	}
}

func TestReadContextCanceled(t *testing.T) {
	d, _ := newDeviceT(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.ReadContext(ctx); err != context.Canceled {
		t.Fatalf("canceled = %v", err)
	}
	if err := d.InitContext(ctx); err != context.Canceled {
		t.Fatalf("init canceled = %v", err)
	}
}

func TestNoCalib(t *testing.T) {
	f := newFake()
	d := New(f)
	// Without Init the calibration is missing.
	if _, err := d.Pressure(); err != ErrNoCalib {
		t.Fatalf("no calib = %v", err)
	}
}

func TestCompensate(t *testing.T) {
	c := calib{
		t1: 27504, t2: 26435, t3: -1000,
		p1: 36477, p2: -10685, p3: 3024, p4: 2855, p5: 140,
		p6: -7, p7: 15500, p8: -14600, p9: 6000,
	}
	temp, press, err := compensate(519888, 415148, &c)
	if err != nil {
		t.Fatal(err)
	}
	if temp != 2508 {
		t.Fatalf("temperature = %d, want 2508 (0.01 °C)", temp)
	}
	if press != 100653 {
		t.Fatalf("pressure = %d, want 100653 Pa", press)
	}
}

func TestMeasureTime(t *testing.T) {
	d := New(&fakeBus{})
	if got := d.measureTime(); got <= 0 || got > 100*time.Millisecond {
		t.Fatalf("measure time = %v", got)
	}
}
