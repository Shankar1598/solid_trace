import { describe, expect, it } from 'vitest'
import { addedEntries, parseEntries } from './capturedRepositories'

describe('parseEntries', () => {
  it('takes one entry on each line, as Console stores it', () => {
    expect(parseEntries(' GitHub.com/Acme/Shop.git \n\ngitlab.example.com/platform/*\n')).toEqual([
      'github.com/Acme/Shop',
      'gitlab.example.com/platform/*',
    ])
  })

  it('keeps each entry once', () => {
    expect(parseEntries('github.com/acme/shop\nGITHUB.COM/acme/shop.git')).toEqual(['github.com/acme/shop'])
  })

  it('gives no entries for empty text', () => {
    expect(parseEntries('  \n ')).toEqual([])
  })
})

describe('addedEntries', () => {
  it('finds the entries that the saved list does not have', () => {
    expect(addedEntries(['github.com/acme/shop'], ['github.com/acme/shop', 'github.com/acme/api'])).toEqual([
      'github.com/acme/api',
    ])
  })

  it('finds none when entries are only removed', () => {
    expect(addedEntries(['github.com/acme/shop', 'github.com/acme/api'], ['github.com/acme/api'])).toEqual([])
  })
})
