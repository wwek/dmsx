import { test } from 'node:test'
import assert from 'node:assert/strict'
import { errorMessage, loadWithError } from '../src/lib/load.ts'

// A failed load has to leave a message behind, otherwise the console looks
// like a machine without data.
test('a failed load keeps its message', async () => {
  let message = ''
  await loadWithError(
    async () => {
      throw new Error('the server is not reachable')
    },
    (text) => {
      message = text
    },
  )
  assert.equal(message, 'the server is not reachable')
})

// A failed load must not throw, or the caller stops before it can show
// anything.
test('a failed load does not throw', async () => {
  await loadWithError(
    async () => {
      throw new Error('boom')
    },
    () => {},
  )
})

// The message is cleared when the load works again.
test('a load that works clears the message', async () => {
  let message = 'the old error'
  await loadWithError(async () => {}, (text) => {
    message = text
  })
  assert.equal(message, '')
})

test('a throw that is not an error still has a message', () => {
  assert.equal(errorMessage('plain text'), 'plain text')
  assert.equal(errorMessage(new Error('boom')), 'boom')
})
