import { randomBytes, randomUUID } from 'node:crypto'
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'

// Fixture transcripts for the Agent sessions flow, in the shape of Claude
// Code's .jsonl files: each line has the cwd and the session id, a reply has
// its model and usage, and a subagent has its own file under
// <session>/subagents/. The flow writes them into a Claude projects folder,
// and the real `solidtrace capture` uploads them. Times are a few minutes
// before now, so the Agent sessions page's 7-day range holds them.

export const MODEL = 'claude-opus-5-5'
export const SUBAGENT_MODEL = 'claude-sonnet-5-5'

/** The tokens of one request, by kind. */
export type Usage = { input: number; output: number; cacheRead: number; cacheWrite: number }

/** A secret in the shape of an Anthropic API key, made at run time so that no key-shaped literal is in the repo. */
export function fakeAnthropicKey(): string {
  return ['sk', 'ant', 'api03'].join('-') + '-' + randomBytes(30).toString('hex')
}

type Line = Record<string, unknown>

/** One transcript: its lines and the line maker that fills the shared fields. */
class Transcript {
  readonly lines: Line[] = []
  private parent: string | null = null
  private at: number

  constructor(private readonly sessionId: string, private readonly cwd: string, start: Date, private readonly extra: Line = {}) {
    this.at = start.getTime()
  }

  private add(fields: Line): string {
    const uuid = randomUUID()
    this.at += 2_000
    this.lines.push({
      parentUuid: this.parent, isSidechain: false, userType: 'external', cwd: this.cwd, sessionId: this.sessionId,
      version: '2.1.325', gitBranch: 'main', ...this.extra, ...fields, uuid, timestamp: new Date(this.at).toISOString(),
    })
    this.parent = uuid
    return uuid
  }

  person(text: string) {
    this.add({ type: 'user', message: { role: 'user', content: text } })
  }

  reply(content: Line[], usage: Usage, model = MODEL) {
    this.add({
      type: 'assistant',
      message: {
        id: `msg_${randomBytes(12).toString('hex')}`, type: 'message', role: 'assistant', model, content,
        stop_reason: content.some(c => c.type === 'tool_use') ? 'tool_use' : 'end_turn',
        usage: {
          input_tokens: usage.input, output_tokens: usage.output,
          cache_read_input_tokens: usage.cacheRead, cache_creation_input_tokens: usage.cacheWrite,
          cache_creation: { ephemeral_5m_input_tokens: usage.cacheWrite, ephemeral_1h_input_tokens: 0 },
        },
      },
    })
  }

  toolResult(toolUseId: string, text: string, toolUseResult: Line = {}) {
    this.add({
      type: 'user', toolUseResult,
      message: { role: 'user', content: [{ type: 'tool_result', tool_use_id: toolUseId, content: text }] },
    })
  }

  text(): string {
    return this.lines.map(l => JSON.stringify(l)).join('\n') + '\n'
  }
}

/** A fixture Agent session: its id and its files, by path relative to the Claude project folder. */
export type FixtureSession = { id: string; files: Record<string, string> }

/** A short Agent session: one person message and one reply. */
export function shortSession(cwd: string, text: string, usage: Usage, start = minutesAgo(20)): FixtureSession {
  const id = randomUUID()
  const main = new Transcript(id, cwd, start)
  main.person(text)
  main.reply([{ type: 'text', text: 'Done.' }], usage)
  return { id, files: { [`${id}.jsonl`]: main.text() } }
}

/**
 * The long Agent session: a person message with a secret, an Agent tool call
 * whose subagent has its own transcript, and a Bash call whose output is
 * past the Log limit of 64 KiB, with a second secret and an end marker
 * after the cut.
 */
export function longSession(cwd: string, opts: {
  personText: string; subagentId: string; subagentReply: string; outputEnd: string; secret: string
  usage: Usage[]; subagentUsage: Usage
}): FixtureSession {
  const id = randomUUID()
  const start = minutesAgo(15)
  const main = new Transcript(id, cwd, start)
  main.person(opts.personText)
  main.reply([{ type: 'tool_use', id: 'toolu_flow_agent', name: 'Agent', input: { description: 'Find the cart code', prompt: 'Find the cart code.' } }], opts.usage[0])

  const sub = new Transcript(id, cwd, new Date(start.getTime() + 3_000), { isSidechain: true, agentId: opts.subagentId })
  // A subagent's prompt is a user line of its own file: not from a person.
  sub.person('Find the cart code.')
  sub.reply([{ type: 'text', text: opts.subagentReply }], opts.subagentUsage, SUBAGENT_MODEL)

  main.toolResult('toolu_flow_agent', opts.subagentReply, { agentId: opts.subagentId, status: 'completed' })
  main.reply([{ type: 'tool_use', id: 'toolu_flow_bash', name: 'Bash', input: { command: 'seq 1 20000', description: 'Print many lines' } }], opts.usage[1])
  const lines = Array.from({ length: 12_000 }, (_, i) => `line ${i + 1} of the output`).join('\n')
  main.toolResult('toolu_flow_bash', `${lines}\nkey ${opts.secret}\n${opts.outputEnd}`)
  main.reply([{ type: 'text', text: 'The cart code is in cart.go, and the command printed its lines.' }], opts.usage[2])

  return {
    id,
    files: {
      [`${id}.jsonl`]: main.text(),
      [`${id}/subagents/agent-${opts.subagentId}.jsonl`]: sub.text(),
    },
  }
}

/** Writes the session's files under the Claude project folder; answers the main transcript's path. */
export function writeSession(projectDir: string, session: FixtureSession): string {
  for (const [path, text] of Object.entries(session.files)) {
    const file = join(projectDir, path)
    mkdirSync(dirname(file), { recursive: true })
    writeFileSync(file, text)
  }
  return join(projectDir, `${session.id}.jsonl`)
}

function minutesAgo(minutes: number): Date {
  return new Date(Date.now() - minutes * 60_000)
}
