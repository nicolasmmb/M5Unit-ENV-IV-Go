# Benchmarks — M5Unit-ENV-IV-Go

## Method

- Host: Apple Silicon (darwin/arm64), Go 1.26.3, `-count=3 -benchtime=100ms`.
- Bus fake (`fakeBus`): register model of the SHT40 and the BMP280.
- The sensor sleeps are part of the measurement, because they are part of the
  API. The pure math is measured separately.

## Results

| Benchmark | Scenario | ns/op | allocs/op |
|---|---|---:|---:|
| `Temperature` | SHT40 read + 10 ms sleep | ~11 ms | 0 |
| `Pressure` | BMP280 forced + ~40 ms sleep | ~44 ms | 0 |
| `Compensate` | BMP280 integer math | 4,1 | 0 |
| `CRC` | Sensirion CRC of 2 bytes | 13,7 | 0 |

The driver overhead is in the tens of nanoseconds; the measurement time of the
sensors dominates. At 1 Hz the cost is irrelevant.

## Footprint

| Type | Size | Budget in the test |
|---|---:|---:|
| `Device` | 96 B | ≤ 128 B |

## Flash (TinyGo, `m5stick-c`)

| Target | code | data | bss | flash | ram |
|---|---:|---:|---:|---:|---:|
| `examples/read` | 22 456 | 140 | 4 628 | 22 596 B | 4 768 B |

## Contracts checked

```bash
make alloc      # zero allocation and footprint
make agnostic   # compiles New for pico and arduino-nano33
```

- `Read`, `Temperature`, `Humidity`, `Pressure`, `SerialNumber`,
  `SetPrecision`, `SetOversampling`, `crc8`, `compensate`: **0 allocs/op**.
- `New` allocates the `Device` once.

## Lesson from the allocation test

The first version allocated **1 object per SHT40 read**. The cause was a slice
literal passed to an interface method:

```go
d.bus.Tx(d.sht, []byte{cmd}, nil)   // the slice escapes: 1 alloc
```

It is now built in the fixed buffer of the device (`d.tx[0] = cmd` and
`d.tx[:1]`). `Pressure` never allocated, which made the bug easy to find.

## Measurements still missing

| Item | Why it matters |
|---|---|
| Real readings against a reference thermometer | Confirms the SHT40 conversion on the unit |
| BMP280 pressure against a reference | Confirms the calibration path |
| Timing of a real Read at 400 kHz | The I2C transfer is not in the host number |
| Heater modes on the unit | They warm the sensor; not tested yet |
