//go:build esp32

// Example: measure how long one Read takes on the device.
//
// It prints the duration of each read in milliseconds and the average of a
// block, so the pipelined read can be compared with the sequential one.
package main

import (
	"time"

	"machine"

	enviv "github.com/nicolasmmb/M5Unit-ENV-IV-Go"
)

const (
	perBlock  = 10
	maxBlocks = 6
)

func main() {
	time.Sleep(time.Second)

	i2c := machine.I2C1
	if err := i2c.Configure(machine.I2CConfig{
		SDA:       machine.GPIO32,
		SCL:       machine.GPIO33,
		Frequency: 100 * machine.KHz,
	}); err != nil {
		println("i2c:", err.Error())
		return
	}

	d := enviv.New(i2c)
	if err := d.Init(); err != nil {
		println("enviv:", err.Error())
		return
	}
	println("timing: default config (SHT40 high, BMP280 x2/x16)")

	for block := 0; block < maxBlocks; block++ {
		start := time.Now()
		var sum time.Duration
		ok := 0
		for i := 0; i < perBlock; i++ {
			t0 := time.Now()
			s, err := d.Read()
			dt := time.Since(t0)
			if err != nil {
				println("read error:", err.Error())
				continue
			}
			_ = s
			sum += dt
			ok++
			println("read", i, "ms", int64(dt/time.Microsecond))
		}
		if ok == 0 {
			println("block", block, "failed")
			continue
		}
		println("block", block, "avg_us", int64(sum/time.Microsecond)/int64(ok),
			"total_ms", int64(time.Since(start)/time.Millisecond))
	}
	println("timing: done")
	for {
		time.Sleep(time.Hour)
	}
}
