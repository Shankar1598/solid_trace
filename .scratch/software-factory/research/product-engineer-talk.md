# Laurie Voss: AI Writes the Code. So What's Left for Engineers?

Source: [youtu.be/kspZP5BgkEU](https://www.youtube.com/watch?v=kspZP5BgkEU), AI Engineer channel, 2026-10-24, 27 minutes. Laurie Voss leads developer relations at Arize AI, which sells evals and observability, and co-founded npm. The notes come from the English subtitles, fetched with yt-dlp on 2026-11-13. He gives his figures without sources ("take my word for it").

## The argument

- **Code is close to free, so the bottleneck moves** (01:15). Following Goldratt's theory of constraints, fixing one bottleneck only moves it. Today it is code review. Next come operations and scaling, then security, and last what agents can't do: deciding what to build and defining what "good" means.
- **Review is the current wall** (02:47–07:49).
  - Agents bring "741% more code" but ship only 30% faster.
  - People review well only up to about 450 lines an hour (Cisco).
  - Four AI reviewers each found real bugs, and no two found the same ones.
  - Cognition's "would a maintainer merge this?" benchmark drops pass rates from 87% to 54%. The missing knowledge (style, side effects, history) "is encoded in the maintainer's brain."
- **Defining "good" doesn't scale** (11:21–14:21). An order tracker for a bakery and one for a factory are different products. The requirements live in the customer's head, and "the only way to pull it out is to talk to them." Brooks' essential complexity, and Sean Grove's "code is a lossy projection of the specification."
- **Whatever can be checked cheaply gets automated** (14:55). Cursor's BugBot turns every thumbs-down into a rule and has about 44,000. Once judgment is written down, the bottleneck moves again.
- **The job becomes "product engineer"** (16:25). One person finds out what people need, defines good, and ships with agents. Juniors need a new training path, and he says the industry should have started two years ago.

## What he tells teams to do (25:02)

1. Isolate critical code and review only that; automated reviewers do the rest.
2. Delegate whatever is easy to verify, and invest in automated ways of judging quality instead of manual review.
3. Turn your best experts' judgment into specs, criteria and evals.
4. Measure the right things: what you actually ship, how often review comments are acted on, and the gap between tests passing and a maintainer merging.
5. "The last reviewer is production." Watch what happens there, with another model judging it.

## For SolidTrace

- "The last reviewer is production" is SolidTrace's existing job: Issues, Logs and Spans from the running system.
- Warp's factory metrics stop at merge (autonomy, cycle time, cost per PR). Joining an agent's run to what its change did in production (new Issues, error rates, slower Transactions after the release) measures past the merge. Events already carry `release` (`event_store/processing/processor.go:360`).
- His step 3 is Warp's Scorers and Poteto's "turn every repeated mistake into a lint rule". His step 4's metrics need the GitHub join from the map's stage 2.
