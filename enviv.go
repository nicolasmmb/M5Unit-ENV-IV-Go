// Package enviv drives the M5Stack Unit ENV-IV (SKU U001-D): an SHT40
// temperature/humidity sensor and a BMP280 pressure sensor behind one Grove
// connector.
//
// The driver is pure Go, uses fixed buffers and performs no heap allocation per
// read. It depends only on the two-argument I2C interface, so machine.I2C
// satisfies it on any chip and a fake bus makes it testable on the host.
package enviv

import (
	"context"
	"errors"
	"sync"
	"time"
)

// I2C is the subset of tinygo.org/x/drivers.I2C used by the unit.
// machine.I2C satisfies it.
type I2C interface {
	Tx(addr uint16, w, r []byte) error
}

const (
	// SHT40Address is the 7-bit address of the temperature/humidity sensor.
	SHT40Address uint16 = 0x44
	// BMP280Address is the 7-bit address of the pressure sensor.
	BMP280Address uint16 = 0x76
)

// Register and command map.
const (
	shtMeasureHigh   = 0xFD // no heater, high repeatability, 10 ms max
	shtMeasureMedium = 0xF6 // 5 ms
	shtMeasureLow    = 0xE0 // 2 ms
	shtMeasureHeat1s = 0x39 // heater 1 s
	shtMeasureHeat1  = 0x32 // heater 100 ms
	shtSerial        = 0x89
	shtSoftReset     = 0x94

	bmpChipIDReg = 0xD0
	bmpChipID    = 0x58
	bmpResetReg  = 0xE0
	bmpResetVal  = 0xB6
	bmpCtlReg    = 0xF4
	bmpConfigReg = 0xF5
	bmpDataReg   = 0xF7
	bmpCalibReg  = 0x88
	bmpCalibLen  = 24
	bmpDataLen   = 6

	bmpModeForced = 0b01
)

// Errors returned by the driver. They are sentinels: compare with errors.Is.
var (
	ErrNotConnected = errors.New("enviv: sensor not responding")
	ErrBadModel     = errors.New("enviv: unexpected BMP280 chip id")
	ErrCRC          = errors.New("enviv: SHT40 CRC mismatch")
	ErrNoCalib      = errors.New("enviv: BMP280 calibration is not loaded")
)

// Precision selects the SHT40 repeatability and the heater.
type Precision uint8

const (
	// High is the default: 10 ms worst case, best accuracy.
	High Precision = iota
	// Medium: 5 ms.
	Medium
	// Low: 2 ms.
	Low
	// Heater1s runs the heater for one second (condensation removal).
	Heater1s
	// Heater100ms runs the heater for 100 ms.
	Heater100ms
)

func (p Precision) command() byte {
	switch p {
	case Medium:
		return shtMeasureMedium
	case Low:
		return shtMeasureLow
	case Heater1s:
		return shtMeasureHeat1s
	case Heater100ms:
		return shtMeasureHeat1
	default:
		return shtMeasureHigh
	}
}

func (p Precision) delay() time.Duration {
	switch p {
	case Medium:
		return 6 * time.Millisecond
	case Low:
		return 3 * time.Millisecond
	case Heater1s:
		return 1100 * time.Millisecond
	case Heater100ms:
		return 120 * time.Millisecond
	default:
		return 10 * time.Millisecond
	}
}

// Sample is one reading of the unit.
//
// Units are integers, so the caller decides about float conversions:
//
//	Temperature  milli degrees Celsius (-40000..125000)
//	Humidity     milli percent RH (0..100000)
//	Pressure     pascals (30000..110000)
type Sample struct {
	Temperature int32
	Humidity    int32
	Pressure    uint32
}

// Celsius returns the temperature in degrees Celsius.
func (s Sample) Celsius() float32 { return float32(s.Temperature) / 1000 }

// RH returns the relative humidity in percent.
func (s Sample) RH() float32 { return float32(s.Humidity) / 1000 }

// HPa returns the pressure in hectopascals.
func (s Sample) HPa() float32 { return float32(s.Pressure) / 100 }

// calib is the BMP280 factory calibration.
type calib struct {
	t1                             uint16
	t2, t3                         int16
	p1                             uint16
	p2, p3, p4, p5, p6, p7, p8, p9 int16
	loaded                         bool
}

// Device is one ENV-IV unit. It is safe for concurrent use.
type Device struct {
	mu   sync.Mutex
	bus  I2C
	sht  uint16
	bmp  uint16
	prec Precision
	osT  uint8 // BMP280 oversampling of temperature (0..5)
	osP  uint8 // BMP280 oversampling of pressure (0..5)
	cal  calib
	tx   [8]byte
	rx   [24]byte
}

// New returns a Device at the factory addresses. Call Init before reading.
func New(bus I2C) *Device {
	return &Device{
		bus:  bus,
		sht:  SHT40Address,
		bmp:  BMP280Address,
		prec: High,
		osT:  2, // x2
		osP:  5, // x16
	}
}

// NewAt returns a Device at the given addresses. Sensors that answer on the
// other address are handled by the same driver.
func NewAt(bus I2C, shtAddr, bmpAddr uint16) (*Device, error) {
	d := New(bus)
	d.sht, d.bmp = shtAddr, bmpAddr
	if err := d.Init(); err != nil {
		return nil, err
	}
	return d, nil
}

// SetPrecision selects the SHT40 repeatability (or the heater mode).
func (d *Device) SetPrecision(p Precision) {
	d.mu.Lock()
	d.prec = p
	d.mu.Unlock()
}

// SetOversampling sets the BMP280 oversampling for temperature and pressure.
// Values are clamped to 0..5 (skip, x1, x2, x4, x8, x16).
func (d *Device) SetOversampling(temp, press uint8) {
	if temp > 5 {
		temp = 5
	}
	if press > 5 {
		press = 5
	}
	d.mu.Lock()
	d.osT, d.osP = temp, press
	d.mu.Unlock()
}

// Init resets both sensors, checks the BMP280 id and loads the calibration.
func (d *Device) Init() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.initLocked(context.Background())
}

// InitContext is Init with cancellation checks between the steps.
func (d *Device) InitContext(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.initLocked(ctx)
}

func (d *Device) initLocked(ctx context.Context) error {
	if d.bus == nil {
		return ErrNotConnected
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// SHT40: soft reset. The sensor needs 1 ms and answers no command while
	// resetting, so a read error right after is not fatal.
	d.tx[0] = shtSoftReset
	if err := d.bus.Tx(d.sht, d.tx[:1], nil); err != nil {
		return ErrNotConnected
	}
	time.Sleep(time.Millisecond)
	if err := ctx.Err(); err != nil {
		return err
	}

	// BMP280: reset, wait for the startup time, then identify.
	if err := d.writeReg(d.bmp, bmpResetReg, bmpResetVal); err != nil {
		return ErrNotConnected
	}
	time.Sleep(3 * time.Millisecond)
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := d.readReg(d.bmp, bmpChipIDReg)
	if err != nil {
		return ErrNotConnected
	}
	if id != bmpChipID {
		return ErrBadModel
	}

	// Calibration block: 24 bytes from 0x88.
	d.tx[0] = bmpCalibReg
	if err := d.bus.Tx(d.bmp, d.tx[:1], d.rx[:bmpCalibLen]); err != nil {
		return ErrNotConnected
	}
	d.cal = parseCalib(d.rx[:bmpCalibLen])
	d.cal.loaded = true

	// IIR filter off, standby 0.5 ms.
	if err := d.writeReg(d.bmp, bmpConfigReg, 0x00); err != nil {
		return ErrNotConnected
	}
	// Sleep mode with the oversampling set; Read starts a forced measurement.
	ctrl := d.osT<<5 | d.osP<<2
	return d.writeReg(d.bmp, bmpCtlReg, ctrl)
}

func parseCalib(b []byte) calib {
	u16 := func(i int) uint16 { return uint16(b[i]) | uint16(b[i+1])<<8 }
	i16 := func(i int) int16 { return int16(u16(i)) }
	return calib{
		t1: u16(0), t2: i16(2), t3: i16(4),
		p1: u16(6), p2: i16(8), p3: i16(10), p4: i16(12),
		p5: i16(14), p6: i16(16), p7: i16(18), p8: i16(20), p9: i16(22),
	}
}

// Read returns one sample: both sensors are read once.
func (d *Device) Read() (Sample, error) {
	return d.ReadContext(context.Background())
}

// ReadContext reads both sensors in one pipeline: the BMP280 forced measurement
// starts first, the SHT40 measurement runs while the BMP280 converts, and the
// remaining BMP280 time is waited at the end. With the default configuration
// that is ~43 ms instead of ~53 ms.
func (d *Device) ReadContext(ctx context.Context) (s Sample, err error) {
	if err := ctx.Err(); err != nil {
		return Sample{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	start := time.Now()

	// The BMP280 converts on its own after the forced-mode write, so start it
	// first and use its conversion time while the SHT40 is measured.
	if err := d.bmpStartLocked(); err != nil {
		return Sample{}, err
	}
	if err := d.shtStartLocked(); err != nil {
		return Sample{}, err
	}

	time.Sleep(d.prec.delay())
	if err := ctx.Err(); err != nil {
		return Sample{}, err
	}
	t, rh, shtErr := d.shtFinishLocked()
	if shtErr == nil {
		s.Temperature, s.Humidity = t, rh
	}

	// Wait only what is left of the BMP280 conversion. The elapsed time is
	// measured, not assumed, so a slow SHT40 read shortens this sleep.
	if remaining := d.measureTime() - time.Since(start); remaining > 0 {
		time.Sleep(remaining)
	}
	_, p, bmpErr := d.bmpFinishLocked()
	if bmpErr == nil {
		s.Pressure = p
	}

	switch {
	case shtErr != nil:
		return Sample{}, shtErr
	case bmpErr != nil:
		return Sample{}, bmpErr
	}
	return s, nil
}

// Temperature reads only the SHT40, in milli degrees Celsius.
func (d *Device) Temperature() (int32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, _, err := d.readSHTLocked(context.Background())
	return t, err
}

// Humidity reads only the SHT40, in milli percent RH.
func (d *Device) Humidity() (int32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, rh, err := d.readSHTLocked(context.Background())
	return rh, err
}

// Pressure reads only the BMP280, in pascals.
func (d *Device) Pressure() (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, p, err := d.readBMPLocked(context.Background())
	return p, err
}

// SerialNumber returns the SHT40 serial number.
func (d *Device) SerialNumber() (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tx[0] = shtSerial
	if err := d.bus.Tx(d.sht, d.tx[:1], nil); err != nil {
		return 0, ErrNotConnected
	}
	time.Sleep(2 * time.Millisecond)
	if err := d.bus.Tx(d.sht, nil, d.rx[:6]); err != nil {
		return 0, ErrNotConnected
	}
	if crc8(d.rx[0:2]) != d.rx[2] || crc8(d.rx[3:5]) != d.rx[5] {
		return 0, ErrCRC
	}
	return uint32(d.rx[0])<<24 | uint32(d.rx[1])<<16 | uint32(d.rx[3])<<8 | uint32(d.rx[4]), nil
}

// readSHTLocked runs one complete SHT40 measurement. The caller holds the mutex.
func (d *Device) readSHTLocked(ctx context.Context) (tMilli, rhMilli int32, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if err := d.shtStartLocked(); err != nil {
		return 0, 0, err
	}
	time.Sleep(d.prec.delay())
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	return d.shtFinishLocked()
}

// shtStartLocked sends the measurement command. The result is ready after
// Precision.delay(). The caller holds the mutex.
func (d *Device) shtStartLocked() error {
	d.tx[0] = d.prec.command()
	if err := d.bus.Tx(d.sht, d.tx[:1], nil); err != nil {
		return ErrNotConnected
	}
	return nil
}

// shtFinishLocked reads and converts the pending measurement. The caller holds
// the mutex.
func (d *Device) shtFinishLocked() (tMilli, rhMilli int32, err error) {
	if err := d.bus.Tx(d.sht, nil, d.rx[:6]); err != nil {
		return 0, 0, ErrNotConnected
	}
	if crc8(d.rx[0:2]) != d.rx[2] || crc8(d.rx[3:5]) != d.rx[5] {
		return 0, 0, ErrCRC
	}
	rawT := uint16(d.rx[0])<<8 | uint16(d.rx[1])
	rawRH := uint16(d.rx[3])<<8 | uint16(d.rx[4])

	// Datasheet: T = -45 + 175*raw/65535, RH = -6 + 125*raw/65535.
	tMilli = int32(int64(-45000) + int64(175000)*int64(rawT)/65535)
	rhMilli = int32(int64(-6000) + int64(125000)*int64(rawRH)/65535)
	if rhMilli < 0 {
		rhMilli = 0
	}
	if rhMilli > 100000 {
		rhMilli = 100000
	}
	return tMilli, rhMilli, nil
}

// readBMPLocked runs one complete BMP280 measurement. The caller holds the mutex.
func (d *Device) readBMPLocked(ctx context.Context) (tempCenti int32, pressPa uint32, err error) {
	if err := d.bmpStartLocked(); err != nil {
		return 0, 0, err
	}
	time.Sleep(d.measureTime())
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	return d.bmpFinishLocked()
}

// bmpStartLocked starts a forced measurement. The result is ready after
// measureTime(). The caller holds the mutex.
func (d *Device) bmpStartLocked() error {
	if !d.cal.loaded {
		return ErrNoCalib
	}
	ctrl := d.osT<<5 | d.osP<<2 | bmpModeForced
	if err := d.writeReg(d.bmp, bmpCtlReg, ctrl); err != nil {
		return ErrNotConnected
	}
	return nil
}

// bmpFinishLocked reads and compensates the pending measurement. The caller
// holds the mutex.
func (d *Device) bmpFinishLocked() (tempCenti int32, pressPa uint32, err error) {
	d.tx[0] = bmpDataReg
	if err := d.bus.Tx(d.bmp, d.tx[:1], d.rx[:bmpDataLen]); err != nil {
		return 0, 0, ErrNotConnected
	}
	rawP := int64(uint32(d.rx[0])<<12 | uint32(d.rx[1])<<4 | uint32(d.rx[2])>>4)
	rawT := int64(uint32(d.rx[3])<<12 | uint32(d.rx[4])<<4 | uint32(d.rx[5])>>4)
	return compensate(rawT, rawP, &d.cal)
}

// measureTime is the worst case measurement time of the BMP280 for the current
// oversampling: 2.3 ms per oversampled sample plus the startup time.
func (d *Device) measureTime() time.Duration {
	samples := func(os uint8) int64 {
		if os == 0 {
			return 0
		}
		return int64(1) << (os - 1)
	}
	ms := time.Duration(1250+samples(d.osT)*2300+samples(d.osP)*2300+500) * time.Microsecond
	return ms
}

// compensate implements Bosch datasheet 3.11.3 in integer math. The pressure
// result is in 1/256 Pa, as in the reference implementation.
func compensate(rawT, rawP int64, c *calib) (int32, uint32, error) {
	t1 := int64(c.t1)
	var1 := (((rawT >> 3) - (t1 << 1)) * int64(c.t2)) >> 11
	var2 := ((((rawT >> 4) - t1) * ((rawT >> 4) - t1)) >> 12) * int64(c.t3) >> 14
	tFine := var1 + var2

	tempCenti := int32((tFine*5 + 128) >> 8)

	p1 := int64(c.p1)
	var1 = tFine - 128000
	var2 = var1 * var1 * int64(c.p6)
	var2 += (var1 * int64(c.p5)) << 17
	var2 += int64(c.p4) << 35
	var1 = ((var1 * var1 * int64(c.p3)) >> 8) + ((var1 * int64(c.p2)) << 12)
	var1 = (((int64(1) << 47) + var1) * p1) >> 33
	if var1 == 0 {
		return tempCenti, 0, nil
	}
	p := int64(1048576) - rawP
	p = (((p << 31) - var2) * 3125) / var1
	var1 = (int64(c.p9) * (p >> 13) * (p >> 13)) >> 25
	var2 = (int64(c.p8) * p) >> 19
	p = ((p + var1 + var2) >> 8) + (int64(c.p7) << 4)
	if p < 0 {
		p = 0
	}
	return tempCenti, uint32(p / 256), nil
}

func (d *Device) readReg(addr, reg uint16) (byte, error) {
	d.tx[0] = byte(reg)
	if err := d.bus.Tx(addr, d.tx[:1], d.rx[:1]); err != nil {
		return 0, err
	}
	return d.rx[0], nil
}

func (d *Device) writeReg(addr, reg uint16, value byte) error {
	d.tx[0] = byte(reg)
	d.tx[1] = value
	return d.bus.Tx(addr, d.tx[:2], nil)
}

// crc8 is the Sensirion CRC: polynomial 0x31, initial value 0xFF.
func crc8(b []byte) byte {
	crc := byte(0xFF)
	for _, v := range b {
		crc ^= v
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x31
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
