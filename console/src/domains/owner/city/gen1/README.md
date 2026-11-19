# City, first generation (frozen 2026-11-19)

The Owner, 2026-11-19: the first generation stops here; the second starts in
its own folder and takes what was good from this one. Nothing here changes
any more except to keep it building; new work goes to `../gen2/`.

Worth taking forward:

- `kit.ts`: the isometric drawing kit (`iso`, faces, boxes, cylinders).
- `landmarks.ts`: the 2.5D sprites — the generator sets by OS (marks, roof
  numbers, wall names, window gauges, stacks), booster, ships.
- `cityMachines.ts`, `cityUtilities.ts`: machines and their supply / demand
  from Prometheus and the topology, with tiers and conflicts.
- `cityFlights.ts`: today's launches from the delivery runs, and replay.
- `worldDictionary.ts`: `steady()` hysteresis and the rule that every object
  stands for a reading, or shows unknown.

Left behind: the free builder (zones, Land −, placing anything anywhere),
the one big map as the only view.
