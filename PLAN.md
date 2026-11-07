# PLAN — M5Unit-ENV-IV-Go

Driver Go do **Unit ENV-IV** (SKU U001-D): SHT40 (0x44) + BMP280 (0x76).
100% Go, sem C, sem CGo, sem Arduino.

- Módulo: `github.com/nicolasmmb/M5Unit-ENV-IV-Go`
- Estado: v0.1.0 implementada. Falta validação em hardware.

## 1. Hardware

| Item | Valor |
|---|---|
| Temperatura/umidade | SHT40, 0x44 |
| Pressão | BMP280, 0x76 |
| SHT40 | −40 a 125 °C, 0 a 100 %RH, ±0,2 °C, ±1,8 %RH |
| BMP280 | 300 a 1100 hPa, ±0,12 hPa |
| Conector | Grove PORT A (GND, 5 V, SDA, SCL) |

## 2. Paridade com as libs oficiais

| Oficial | Esta lib |
|---|---|
| `SHT4x` comm 0xFD/0xF6/0xE0 + heater | `Precision` High/Medium/Low/Heater1s/Heater100ms |
| `SHT4x` serial 0x89, reset 0x94 | `SerialNumber`, `Init` |
| `SHT4x` CRC Sensirion | `ErrCRC` |
| `BMP280` calibração 24 bytes em 0x88 | `parseCalib` |
| `BMP280` modo forçado + oversampling | `SetOversampling` |
| `BMP280` algoritmo 3.11.3 (int) | `compensate` |
| `ENVUnit(type=4)` | `New` |

## 3. Escopo

Dentro: leitura combinada, leituras individuais, precisão do SHT40, heater,
oversampling do BMP280, CRC, presença/id, `context`, testes de host, benchmarks,
exemplo, CI com gate de agnosticismo.

Fora: filtro IIR configurável, uso do pino de interrupção, calibração de
altitude, e o BMP280 como referência de temperatura (o SHT40 é melhor).

## 4. API

Ver README. Resumo: `New`, `NewAt`, `Init`, `Read`, `Temperature`, `Humidity`,
`Pressure`, `SerialNumber`, `SetPrecision`, `SetOversampling`, `Sample`.

## 5. Testes

Fake com modelo de registrador dos dois sensores:

- presença, id errado, sensor mudo, barramento nulo;
- CRC corrompido;
- calibração de referência Bosch (25,08 °C / 100 653 Pa);
- precisão chega ao SHT40 (comando gravado);
- oversampling chega ao BMP280 (`ctrl_meas`);
- cancelamento de contexto;
- 0 alocação e footprint 96 B.

## 6. Fases

- [x] Pesquisa (datasheets, libs oficiais, endereços).
- [x] Núcleo, conversões, CRC, compensação.
- [x] Testes, contratos, benchmarks.
- [x] README, BENCHMARKS, Makefile, CI, exemplo.
- [ ] Validação em hardware (flash + leituras reais).
- [ ] Publicar e tag `v0.1.0`.

## 7. Pendências

- [ ] Flash do `examples/read` com o unit, conferir valores plausíveis.
- [ ] Comparar com termômetro de referência.
- [ ] Medir um `Read` a 400 kHz.
- [ ] Expor `SetIIR` se a pressão oscilar demais.

## 8. Referências

- https://docs.m5stack.com/en/unit/Unit_ENV-IV
- `uiflow-micropython/m5stack/libs/driver/sht4x.py`, `bmp280.py`
- `m5stack/M5Unit-ENV` (Arduino)
- Datasheets Sensirion SHT4x e Bosch BMP280 (seção 3.11.3)
