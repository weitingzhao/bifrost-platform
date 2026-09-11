# The data contract on a design

A block in a design is finished when it declares three things. Put the first as the
section's question, and the other two in its source label (right of the header).

| | Question | Source | Verdict |
|---|---|---|---|
| **What** | the question this block answers, in the owner's words | the read that answers it, and how fast | who judged the state it shows |
| **Example** | "Does it hold what 2026-09-10 should?" | `dimensions · freshness · 5 ms cached` | the plugin's freshness axis |

Rules that follow from it:

1. **One snapshot read per page** is the target. Each question should be answerable
   from a read the service has already computed; a block that needs a slow or live
   probe is either cached by the service or moved off the first screen.
2. **The source label is part of the design**, not a developer note. It is what lets
   a reader trust a green and lets an implementer build the page without guessing.
3. **The verdict column is never "the page".** If no service computes the state a
   block needs, the design is asking for a new verdict — raise it as such.
4. **Denominators come from the service**, with the scope named ("of 585 in the
   option universe", "of 5,317 active US common stocks"). A number with no scope
   gets no bar and no percentage.
5. **Loading, failed and empty are three different renderings** (`…`, `Unknown`,
   the service's own empty state) — design all three for every block.

The Massive Overview's full contract is in `reference/massive-overview-contract.md`.
