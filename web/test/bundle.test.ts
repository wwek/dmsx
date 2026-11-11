import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

// The console that reaches the operator is the built one in internal/server/ui.
// A fix that stays in the source and never reaches the bundle helps nobody.
const ui = new URL('../../internal/server/ui/', import.meta.url)

test('the shipped console carries the message of a failed load', () => {
  const html = readFileSync(new URL('index.html', ui), 'utf8')
  const script = /src="\/assets\/(index-[^"]+\.js)"/.exec(html)
  assert.ok(script, 'the console has no script bundle')
  const js = readFileSync(new URL(`assets/${script[1]}`, ui), 'utf8')
  assert.match(js, /The data could not be loaded:/)
  assert.match(js, /数据加载失败：/)
})
