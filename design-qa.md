# Collaboration design QA — 2026-11-22

Direction: Working Studio, selected autonomously under Owner's instruction.
Source image: /Users/vision-mac-trader/.codex/generated_images/01a2057b-ccf6-70d2-aab2-0cd1af1d0b1c/exec-0e705d0f-fb64-497c-a864-602f9fd0c45b.png

Implementation: http://127.0.0.1:5293/communication-preview.html
Evidence captures: /private/tmp/communication-studio-desktop.jpg and
/private/tmp/communication-studio-mobile.jpg. These show illustrative fixture data,
not real work. Desktop and 390px narrow layout inspected; temporary viewport reset.

The implemented direction retains domain-grouped desks, a dark surface, selected
exchange and evidence inspector. Deliberate differences: no invented progress,
active animation or completed status; explicit coverage and independent evidence;
time cutoff is a filter over retained facts, not replayed member presence.

Interactions checked: member filter with empty result, clear filter, Studio/Ledger,
cutoff before ACK, restore latest evidence. Narrow layout stacks inspector and
stations; desktop uses two columns. Unknown activity remains explicit. Earlier
local duplicate React imports were fixed using dependency deduplication; reload
renders correctly without new hook errors.

Visual/interaction checks passed on fixture. Live authenticated source acceptance
is pending login on the local origin; guest errors were observed, not hidden.
This is not a claim of pixel-exact reproduction or completed live system acceptance.

## 2026-11-22 spatial revision

Owner clarified: normal operation needs restrained dynamics and stable entities;
issues must contrast without a world full of flashing signals. Implemented an
independent Phaser office scene in bifrost-ui/communication/OfficeWorld.tsx.
Selected connection only; fixed members; finite evidence-triggered movement;
source-reported issue label plus persistent issue index; no inferred failure.
Existing City geometry/animation are untouched. No new engine dependency added.

Browser verified Play sequence reaches delivery, ACK, then blocked; Show recovery
removes the issue and displays independent completion evidence. Captures:
/private/tmp/communication-world-blocked.png
/private/tmp/communication-world-recovered.png
390px viewport inspected, controls wrap; scene has pan/zoom and equivalent record
access. Mobile overview is necessarily small; this first version is not a final
art pass. Temporary viewport reset after checking.

Seven model/observer tests pass. jsdom reports its missing canvas implementation;
the GPU scene itself was verified in Chrome, not claimed covered by jsdom.
The real adapter has no source-reported issue feed yet; the new issue demonstration
is explicitly illustrative. Authenticated live acceptance is still pending.

Selection references: Software Inc. office/team representation
https://softwareinc.coredumping.com/ and RimWorld autonomous members
https://rimworldgame.com/ . Reuse Phaser for browser scene/camera/input rather than
adding a second renderer: https://www.phaser.io/oss/docs . Borrow the observation
grammar, not either game's simulation, management authority, or artwork.
