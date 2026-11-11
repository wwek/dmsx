// The console has to say that a load failed. A failed load used to be
// swallowed, so the operator saw an empty console instead of the error.

export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// loadWithError runs a load and keeps the message of a failure. The message is
// cleared when the next load works. The load does not throw, so the caller
// keeps running and can show what went wrong.
export async function loadWithError(
  load: () => Promise<void>,
  onError: (message: string) => void,
): Promise<void> {
  try {
    await load()
    onError('')
  } catch (err) {
    onError(errorMessage(err))
  }
}
