/**
 * The room standards (the Owner, 2026-11-19: in making the CPU's room we formed standards; every future
 * room is designed against them, and each is weighed for whether it makes sense). Drawn out of the CPU
 * workshop, one corrected mistake at a time; the same for every unit's room. `check` says how a room is
 * held to it: by a test on its drawing, or by the Owner's eye at the Inside stage.
 */

export type RoomStandard = {
  id: string
  /** The standard, in one line. */
  rule: string
  /** What it means in practice. */
  how: string
  /** Where it came from: the Owner's words, or the mistake it was drawn from (2026-11-19, the CPU room). */
  from: string
  check: 'test' | 'eye'
}

export const ROOM_STANDARDS: RoomStandard[] = [
  {
    id: 'real-meaning', check: 'eye',
    rule: 'Everything in the room means something real; what the hardware itself knows, and nothing else.',
    how: 'No part, light or number that stands for no reading. A unit shows its own hardware, not what it is used for (that belongs to the districts that use it).',
    from: 'Load and waiting were drawn in the CPU room; they are counted by the scheduler, not known by the CPU, and came out. The Owner: anything without a real meaning is noise.',
  },
  {
    id: 'once', check: 'test',
    rule: 'A reading is said once.',
    how: 'One reading, one place, one look. Two shapes for the same reading (a bar and a dial, a colour and a light) is one too many.',
    from: 'Each machine had a load bar and a half dial; machines were grey when idle and green when busy — the load said again, and not in the key.',
  },
  {
    id: 'scope', check: 'test',
    rule: 'A reading is drawn where it is measured.',
    how: 'Per part (a core) on that part; for the whole unit on its console. A sensor says by its label which it measures.',
    from: 'Temperature and load first hung on the wall without saying whether they were per core or for the whole chip.',
  },
  {
    id: 'console', check: 'test',
    rule: 'Every room has a console: the whole unit at a glance.',
    how: 'A control desk against the back wall with what is measured over the whole unit, and the average of its parts. Standard instruments, one slot each; the desk as wide as them; nothing outside it. What the console shows is the room\'s summary — the same that the room passes up to its building (stage 3).',
    from: 'The Owner, after the CPU console: every room can have one, it sums the room up — make it a standard.',
  },
  {
    id: 'no-hiding', check: 'test',
    rule: 'Nothing hides anything else (2.5D).',
    how: 'No part rises above its base further than the gap to the thing behind it or beside it, in screen pixels: (pitch − depth) × 16. Readings lie on top surfaces. Tall things (the console) stand against the back wall.',
    from: 'Tall work lights, then tall headstocks, covered the machines behind; instruments on the back wall were hidden by the machines.',
  },
  {
    id: 'glance', check: 'eye',
    rule: 'The load reads at a glance, and one kind of reading has one look.',
    how: 'A share is lit folds of the thing\'s own part, filled from one end, orange above 85%, dark when idle — the same folds on every machine and on the console.',
    from: 'A tall light was hard to see; a dark bar with ticks looked like a battery. The Owner liked the console\'s bar matching each machine\'s.',
  },
  {
    id: 'real-thing', check: 'eye',
    rule: 'It looks like the real thing, not a box.',
    how: 'Shape gives it away (a lathe: bed, headstock and chuck, tailstock). Shape is structure only: no buttons or lamps for show.',
    from: 'A box with a battery on it; a console like a television.',
  },
  {
    id: 'colour', check: 'test',
    rule: 'Every colour that changes means something, and is in the key.',
    how: 'Structure keeps one colour each and never changes; only readings change colour, each change written in the key.',
    from: 'Grey and green machines that meant something and said it nowhere; a yellow floor line that meant nothing.',
  },
  {
    id: 'standard-parts', check: 'test',
    rule: 'Capacity in standard parts; the room grows with them.',
    how: 'One part one fixed size, city-wide, adjustable by ruler. The room is the smallest allowed size that holds them; parts in pairs and powers of two; spread evenly, neither crowded nor empty.',
    from: 'Machines shrank to fit; three to a row; crowded in a corner with the room empty; rows too far apart.',
  },
  {
    id: 'not-read', check: 'test',
    rule: 'Not read, not drawn.',
    how: 'No needle, bar or number is guessed; and no empty structure stands for what is not read (no instrument bank with nothing in it).',
    from: 'Speed dials on Mac minis that report no clock; an empty instrument bank.',
  },
  {
    id: 'numbers', check: 'test',
    rule: 'Numbers are written one way.',
    how: 'readingText: 7%, 2.4 GHz, 68°C, 500 GB, 24.0 TB; a dash when not read.',
    from: 'The Owner: what can be shown, shown to one standard.',
  },
  {
    id: 'now', check: 'eye',
    rule: 'A room shows this moment.',
    how: 'What the hardware is now: a reading taken now, or a rate over the last few minutes. History (how it was used over days, counts since boot) belongs outside the room, later.',
    from: 'The Owner, choosing the memory room\'s details: the CPU and memory readings are the state now; their history is outside, not here.',
  },
]
