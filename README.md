# M5Unit-ENV-IV-Go

[![test](https://github.com/nicolasmmb/M5Unit-ENV-IV-Go/actions/workflows/test.yml/badge.svg)](https://github.com/nicolasmmb/M5Unit-ENV-IV-Go/actions/workflows/test.yml)

Pure-Go driver for the **M5Stack Unit ENV-IV** (SKU **U001-D**): an **SHT40**
temperature/humidity sensor and a **BMP280** pressure sensor on one Grove
connector. No C, C++, CGo or Arduino.

Board agnostic: the core depends only on `Tx(addr, w, r)`, so `machine.I2C`
satisfies it on any chip and a fake bus makes it testable on the host.

## Features

- One `Read` returns temperature, humidity and pressure from both sensors.
- Integer output in milli-units, so the caller decides about floats.
- SHT40: repeatability (high/medium/low) and the two heater modes.
- BMP280: oversampling for temperature and pressure, forced measurement.
- Sensirion CRC checked on every SHT40 word (`ErrCRC`).
- Chip id check, soft reset and calibration load (`ErrBadModel`, `ErrNoCalib`).
- `ReadContext`/`InitContext` for cancellation.
- Zero heap allocation per read. `Device` is 96 bytes.
- Host tests with a register-level fake of both sensors, allocation and
  footprint contracts, benchmarks, and a CI gate that compiles the package for
  RP2040 and nRF52840.

## Hardware

| Parameter | Value |
|---|---|
| Unit | ENV-IV, SKU U001-D |
| Temperature/humidity | SHT40 at **0x44** |
| Pressure | BMP280 at **0x76** |
| SHT40 range/accuracy | −40..125 °C, 0..100 %RH, ±0.2 °C, ±1.8 %RH |
| BMP280 range/accuracy | 300..1100 hPa, ±0.12 hPa |
| Connector | Grove HY2.0-4P on PORT A (GND, 5 V, SDA, SCL) |

The Grove port is shared with the other units: one unit at a time, or a PaHub.

## Install

```bash
go get github.com/nicolasmmb/M5Unit-ENV-IV-Go@v0.1.0
```

## Quick start

```go
i2c := machine.I2C1
_ = i2c.Configure(machine.I2CConfig{
    SDA: machine.GPIO32, SCL: machine.GPIO33, Frequency: 100 * machine.KHz,
})

d := enviv.New(i2c)
if err := d.Init(); err != nil {
    println("enviv:", err.Error())
    return
}

s, err := d.Read()
if err == nil {
    println("T", s.Temperature, "mC  RH", s.Humidity, "mRH  P", s.Pressure, "Pa")
    println("float:", s.Celsius(), s.RH(), s.HPa())
}
```

## API

| Function | Description |
|---|---|
| `New(bus I2C) *Device` | Factory addresses 0x44 and 0x76 |
| `NewAt(bus, shtAddr, bmpAddr) (*Device, error)` | Other addresses, runs `Init` |
| `Init() error` / `InitContext(ctx)` | Reset both, check id, load calibration |
| `Read() (Sample, error)` / `ReadContext(ctx)` | One reading of both sensors |
| `Temperature() (int32, error)` | SHT40 only, milli °C |
| `Humidity() (int32, error)` | SHT40 only, milli %RH |
| `Pressure() (uint32, error)` | BMP280 only, pascals |
| `SerialNumber() (uint32, error)` | SHT40 serial |
| `SetPrecision(Precision)` | `High` (default), `Medium`, `Low`, `Heater1s`, `Heater100ms` |
| `SetOversampling(temp, press uint8)` | 0..5 (skip, x1..x16), clamped |
| `Sample.Celsius/RH/HPa()` | float helpers over the milli units |

Errors: `ErrNotConnected`, `ErrBadModel`, `ErrCRC`, `ErrNoCalib`.
Compare with `errors.Is`.

## Behaviour notes

- **Units are integers**: `Sample.Temperature` is milli °C (`25080` = 25.08 °C),
  `Humidity` is milli %RH, `Pressure` is pascals. This keeps the read free of
  float math and `fmt`.
- The **SHT40 is the temperature reference**. The BMP280 also reports
  temperature, but it is only used inside its own pressure compensation.
- `Read` sleeps: 10 ms for the SHT40 at high repeatability and up to ~40 ms for
  the BMP280 at x16 oversampling. Lower `SetOversampling` to go faster.
- The SHT40 needs 10 ms (high), 5 ms (medium) or 2 ms (low); the heater modes
  need 1.1 s or 110 ms. These are the worst-case datasheet values, used as the
  sleep.
- Humidity is clamped to 0..100 %, as in the official driver.

## Performance

See [BENCHMARKS.md](BENCHMARKS.md). Host numbers, fake bus:

| Operation | ns/op | allocs/op |
|---|---:|---:|
| `Temperature` (10 ms sleep) | ~11 ms | 0 |
| `Pressure` (40 ms sleep) | ~44 ms | 0 |
| `compensate` (BMP280 math) | 4,1 | 0 |
| `crc8` | 13,7 | 0 |

RAM: `Device` 96 bytes. Flash: `examples/read` 22.6 KB on `m5stick-c`.

## Validation

```bash
make check      # vet + race tests + allocation/footprint + example + agnostic
make read PORT=/dev/cu.usbserial-XXXX
```

`make agnostic` compiles `New` for `pico` and `arduino-nano33`; CI repeats it in
the `agnostic` job.

## Hardware results

Verified on an M5StickC Plus2 with a real Unit ENV-IV (`examples/read`, 1 Hz):

```
SHT40 serial: 208058699
temperature mC: 29341  humidity mRH: 66705  pressure Pa: 94450
temperature mC: 29352  humidity mRH: 66690  pressure Pa: 94450
temperature mC: 29371  humidity mRH: 66672  pressure Pa: 94453
```

- The serial number is read through the CRC-checked path, so the Sensirion CRC
  works on real data.
- Temperature and humidity are stable to a few milli-units between reads.
- Pressure repeats within 3 Pa.
- No `ErrCRC`, `ErrBadModel` or `ErrNoCalib` in continuous operation.

Pressure is absolute, so compare it with the local station pressure (a city at
1100 m shows about 890 hPa).

## Known gaps

| Gap | Effect | Next step |
|---|---|---|
| ~~Not validated on hardware~~ | Validated on a real ENV-IV, see below | Done |
| IIR filter of the BMP280 is off | Noisier pressure than the maximum | Expose `SetIIR` if needed |
| No interrupt use | The SHT40 and BMP280 can signal data ready | Not wired on the unit |
| `Read` sleeps inside the lock | A second goroutine waits | Documented; use `Temperature`/`Pressure` separately |

## References

- Unit ENV-IV (U001-D): https://docs.m5stack.com/en/unit/Unit_ENV-IV
- SHT40 datasheet: commands 0xFD/0xF6/0xE0, heater modes, CRC poly 0x31
- BMP280 datasheet 3.11.3: integer compensation used here
- Official drivers: `uiflow-micropython/m5stack/libs/driver/sht4x.py` and
  `bmp280.py`; Arduino `m5stack/M5Unit-ENV`

## License

MIT — see [LICENSE](LICENSE).
