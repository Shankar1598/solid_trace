import { createHmac } from 'node:crypto'

// An Authenticator app for flows: RFC 6238 codes (6 digits, 30 s, SHA-1)
// from the secret the set-up page shows. Console accepts each step once, so
// codeAfter gives one for the first step after the last one used, waiting
// until Console's clock (one step either side) accepts it.

const STEP = 30

function base32(secret: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const char of secret.replace(/[\s=]/g, '').toUpperCase()) {
    bits += alphabet.indexOf(char).toString(2).padStart(5, '0')
  }
  const bytes = bits.match(/.{8}/g) ?? []
  return Buffer.from(bytes.map(byte => parseInt(byte, 2)))
}

export function codeAt(secret: string, step: number): string {
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(step))
  const hmac = createHmac('sha1', base32(secret)).update(counter).digest()
  const offset = hmac[hmac.length - 1] & 0xf
  const value = (hmac.readUInt32BE(offset) & 0x7fffffff) % 1_000_000
  return value.toString().padStart(6, '0')
}

const nowStep = () => Math.floor(Date.now() / 1000 / STEP)

/** A code for the first step after lastStep (none used: -1), and its step. */
export async function codeAfter(secret: string, lastStep: number): Promise<{ code: string; step: number }> {
  const step = Math.max(nowStep(), lastStep + 1)
  while (step > nowStep() + 1) await new Promise(r => setTimeout(r, 1000))
  return { code: codeAt(secret, step), step }
}
