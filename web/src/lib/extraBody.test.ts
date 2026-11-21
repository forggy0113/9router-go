import { describe, expect, it } from 'bun:test'
import { parseExtraBodyInput } from './extraBody'

describe('parseExtraBodyInput', () => {
  it('treats a blank field as no extraBody', () => {
    expect(parseExtraBodyInput('')).toEqual({ error: '' })
    expect(parseExtraBodyInput('   \n')).toEqual({ error: '' })
  })

  it('accepts a JSON object', () => {
    expect(parseExtraBodyInput(' {"provider":{"order":["anthropic"]}} ')).toEqual({
      value: { provider: { order: ['anthropic'] } },
      error: '',
    })
  })

  it.each(['[1,2]', '"x"', '1', 'null', 'true'])('refuses non-object %s', (raw) => {
    const got = parseExtraBodyInput(raw)
    expect(got.value).toBeUndefined()
    expect(got.error).toBe('Custom request body must be a JSON object.')
  })

  it('reports malformed JSON', () => {
    const got = parseExtraBodyInput('{"a":')
    expect(got.value).toBeUndefined()
    expect(got.error).toStartWith('Invalid JSON:')
  })
})
