//go:build esp32

// Example: print the ENV-IV readings on the serial port.
//
// The package has no board knowledge: you pass the I2C bus.
// On an M5StickC Plus2 the Grove port A is machine.I2C1 on GPIO32/GPIO33.
package main

import (
	"time"

	"machine"

	enviv "github.com/nicolasmmb/M5Unit-ENV-IV-Go"
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
	if sn, err := d.SerialNumber(); err == nil {
		println("SHT40 serial:", sn)
	}

	for {
		s, err := d.Read()
		if err != nil {
			println("enviv:", err.Error())
			time.Sleep(time.Second)
			continue
		}
		println("temperature mC:", s.Temperature, "humidity mRH:", s.Humidity, "pressure Pa:", s.Pressure)
		time.Sleep(2 * time.Second)
	}
}
