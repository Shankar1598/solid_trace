# 04: Text matches run last within an and

**What to build:** When the Query compiler writes an `and`, conditions with no text match come first and those with one after, each group in its written order. That covers an `and` the member wrote and one made by pushing a negation through an `or`. A Query's leading `where` and `search` stages are ordered the same way, a `search` counting as a text match. `or` isn't reordered. So `message contains "x" and rare_key == "y"` matches `message` only on the Logs where `rare_key` is `y`, whichever order it is written in, and the answers don't change. See the [spec](../spec.md)'s Implementation Decisions.

It waits for 03 because both change the same condition writer. Measure the gain with 03's matching in place: if the probe shows little left to gain, say so in the Comments before building it.

**Blocked by:** 01, 03.

**Status:** ready-for-agent

- [ ] The same Query, written with its conditions in each order, lists the same Logs (root tests through the Query route). The conditions are:
  - a text match on `message`;
  - a condition on an Attribute only in the maps;
  - one on a promoted Attribute;
  - one on a key promoted partway through a day.
- [ ] Several `where` and `search` stages give the same Logs as before
- [ ] Negated `or`s with a text match give the same Logs as before
- [ ] The existing Query, Chart and Monitor root tests pass
- [ ] The probe's text-match and map Attribute timings in both orders, before and after, are in this ticket's Comments
