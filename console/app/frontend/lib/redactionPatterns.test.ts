import { describe, expect, it } from 'vitest'
import { parsePatterns } from './redactionPatterns'

describe('parsePatterns', () => {
  it('takes one pattern on each line, with no empty lines', () => {
    expect(parsePatterns('acme_live_[a-z0-9]{16}\r\n\n  \nshop_[A-Z]{8}\n')).toEqual([
      'acme_live_[a-z0-9]{16}',
      'shop_[A-Z]{8}',
    ])
  })

  it('keeps the spaces of a pattern, as they are part of it', () => {
    expect(parsePatterns(' pw [0-9]{6} ')).toEqual([' pw [0-9]{6} '])
  })

  it('gives no pattern for an empty text, which removes them all', () => {
    expect(parsePatterns('')).toEqual([])
  })
})
