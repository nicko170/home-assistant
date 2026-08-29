# Rollease Acmeda / Automate 433.92 MHz blind remote — protocol notes

Reverse-engineered from RTL-SDR captures of a **Automate Push 5 Channel**
remote (FCC ID `2AGGZMT0201014`, IC `21769-MT0201014`) controlling three
outdoor blinds. Captures: 3 presses each of ch1 UP, ch1 STOP, ch2 UP, ch3 UP,
ch1 DOWN, plus separate single-channel captures.

The [Flipper Zero thread][flipper] for this exact FCC ID stalled because nobody
had the radio parameters — the blocker quoted there was needing "bit rate,
preamble, synchronization word, deviation, number of bits in the packet". All
of those are below. The crypto is not broken, and this documents why it can't
be from RF alone.

## Physical layer

| Parameter | Value |
|---|---|
| Centre frequency | 433.92 MHz |
| Modulation | 2-FSK / GFSK |
| Deviation | ±24 kHz (47–51 kHz tone separation) |
| Symbol rate | **39 830 baud** (±170 measured across 35 frames) |
| Encoding | NRZ (run lengths of 3–4 symbols occur, so not Manchester) |
| Preamble | ~132–140 ms of alternating symbols (≈5 700 symbols) |

The symbol rate is best measured from the preamble: alternating symbols are a
square wave at exactly half the symbol rate in the instantaneous-frequency
signal, so a 130 ms FFT resolves the rate to ~15 Hz. Do not estimate it from
run lengths — quantising to integers gives 6.0 samples/symbol for anything
between 39.4 k and 46.5 k baud.

The long preamble is a wake-on-radio duty-cycle preamble; the receiver samples
periodically and needs to catch a carrier. Budget for it when transmitting.

## Frame structure

One button press transmits:

```
[~140 ms preamble][DATA][DATA]  [~140 ms preamble][DATA][DATA][DATA]…
```

Each `DATA` block is ~25 ms: a **~205-bit packet** followed by ~20 ms of
unmodulated carrier. A press emits **two distinct messages, each repeated 2–3
times**. Repeated copies are bit-identical (waveform cross-correlation
r = 0.95–0.97); the two distinct messages correlate at r ≈ 0.50.

> That 0.50 is a trap. Comparing message A of one press against message B of
> another returns ~50% similarity and looks exactly like a rolling code even
> when the demodulator is perfect. Cluster blocks by waveform correlation
> *before* concluding anything.

## Packet layout

```
bit   0 ─────────────────── 58   59 ──────────────────── 199
    ┌──────────────────────────┬─────────────────────────────┐
    │  fixed header, 59 bits   │  variable payload, 141 bits │
    │  45 BA 85 BA 82 42 58 +3 │  changes every press        │
    └──────────────────────────┴─────────────────────────────┘
```

The 59-bit header is **byte-identical across every channel and every button**
(ch1/ch2/ch3, UP/STOP/DOWN) — it is a sync word plus system/remote identifier,
not a channel or command field. Channel and command live inside the encrypted
region.

### The payload is encrypted, not a counter

Measured over 22 cleanly-demodulated packets:

| Test | Result | Interpretation |
|---|---|---|
| Mean P(bit=1) across 141 positions | 0.509 | fair coin |
| Positions with bias <15% or >85% | none | no fixed subfields |
| Pairwise Hamming distance between presses | 45–62%, centred 50% | **strict avalanche** |

The Hamming distance is decisive. A counter changes 1–5 bits between
consecutive presses. A scrambler or PN9 whitener is deterministic and changes
none. Flipping ~50% of bits on every press — including two presses of the same
button four seconds apart — is what a block cipher does. 141 bits is consistent
with AES-128 plus a short CRC.

Do not bother with the chi-square figure (301, df=255) — at 374 sampled bytes
the expected count per bin is 1.46, far below the ≥5 the test needs.

**Consequence: no quantity of RF captures will yield the key.** Replay is dead,
and so is analytic recovery. This is the protocol working as designed.

## Where the remaining attack surface is

FCC internal photos (exhibit `Internal-picture-rev1-4292465`) show a single
MCU+radio SoC with a PCB trace antenna, and **a row of ~5 test pads on the
switch side plus further pads beside the SoC** — a debug/programming header.
If flash readout protection is not set, the key and algorithm are extractable
from firmware. That is the only route to a from-scratch transmitter.

## Methodology notes

Two wrong conclusions were reached before this one, both from unvalidated
decoders:

1. **"Fixed code", from 99% run-length similarity.** ~98% of the compared runs
   were preamble. Two unrelated commands would also have scored ~98%.
2. **"Rolling code", from ~50% payload similarity.** Correct answer, invalid
   evidence — it was comparing message A against message B, and the varying
   packet lengths (103/107/111 runs) were decoder noise, not protocol.

The check that settles it: **blocks within a single press must be identical**,
because no counter can advance between transmissions 28 ms apart. Correlate raw
FM waveforms — no clock recovery, nothing to drift. If same-press blocks don't
correlate ~1.0, the front end is broken and no fixed-vs-rolling claim is
admissible. Average the verified-identical copies before slicing; it removes
almost all bit errors and stops single-bit noise from masquerading as fields.

Scripts: `rfsoft.py` (baud/preamble), `rfstruct.py` (press anatomy),
`rfblocks.py` (within-press validation), `rfpacket.py` (averaged extraction),
`rffields.py` (field boundaries and entropy).

[flipper]: https://forum.flipper.net/t/rollease-acmeda-433-92mhz-roller-blind-remote-fcc-2aggzmt0201014/8119
