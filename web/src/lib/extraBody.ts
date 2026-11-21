/**
 * Parses the Add Custom Model "Custom request body" field. Blank means no
 * extraBody; anything else must be a JSON object. The gateway re-validates
 * (size, reserved keys), so this only catches what the operator can fix inline.
 */
export function parseExtraBodyInput(raw: string): { value?: Record<string, unknown>; error: string } {
  const text = raw.trim()
  if (!text) return { error: '' }
  let v: unknown
  try {
    v = JSON.parse(text)
  } catch (err) {
    return { error: `Invalid JSON: ${err instanceof Error ? err.message : String(err)}` }
  }
  return v && typeof v === 'object' && !Array.isArray(v)
    ? { value: v as Record<string, unknown>, error: '' }
    : { error: 'Custom request body must be a JSON object.' }
}
