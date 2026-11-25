# Models for each role

The repository can set the model and effort of each role in `.solidtrace/review.toml` at its root. A role with no line, or a repository with no file, uses the default below.

```toml
[models]
bugs-and-spec = "inherit"
tests = "inherit"
code-quality = "sonnet high"
verifier = "inherit"
ha-and-infrastructure = "inherit"
security = "inherit"
deep-review = ["opus xhigh", "opus xhigh"]
record = "sonnet"
sessions = "haiku"
```

Each value is a model alias (`opus`, `sonnet`, `haiku` or `fable`) and an optional effort (`low`, `medium`, `high`, `xhigh` or `max`). `inherit` is this session's model. `deep-review` is a list: one adversary for each entry.

| Role | Default | Used by |
|---|---|---|
| `bugs-and-spec` | `inherit` | code-review |
| `tests` | `inherit` | code-review |
| `code-quality` | `sonnet high` | code-review |
| `verifier` | `inherit` | code-review, update-pr |
| `ha-and-infrastructure` | `inherit` | open-pr |
| `security` | `inherit` | open-pr, update-pr |
| `deep-review` | `["opus xhigh", "opus xhigh"]` | deep-review |
| `record` | `sonnet` | open-pr, update-pr |
| `sessions` | `haiku` | open-pr, update-pr |

When you spawn a role, pass its alias as `model` (no `model` for `inherit`) and its effort as `effort` (no `effort` when the value has none). In the block, write the alias that ran (for `inherit`, this session's alias) and the effort that you passed, or `default`.

A value that is not valid, such as an unknown alias, uses the default. Say so in your reply.
