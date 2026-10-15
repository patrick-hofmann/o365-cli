import type { MailStore, ProgressInput } from './store.js'

export interface ProgressContext {
  input: { checkpointRevision: number, checkpoint: Record<string, unknown> }
  progress: { commit: (input: Omit<ProgressInput, 'podId'>) => Promise<{ revision: number }> }
}

/** Publish retained evidence through the generic checkpoint API. Resume after interrupted publication. */
export function knowledgePublisher(store: MailStore, namespace: string, context: ProgressContext) {
  let revision = context.input.checkpointRevision
  let checkpoint = { ...context.input.checkpoint }
  return async () => {
    const marker = checkpoint.externalMailKnowledge as { row?: unknown } | undefined
    const after = Number.isSafeInteger(marker?.row) ? Number(marker!.row) : 0
    const rows = store.db.prepare('SELECT rowid AS sequence,* FROM claims WHERE pod_id=? AND rowid>? ORDER BY rowid').all(namespace, after)
    for (const row of rows) {
      const citations = JSON.parse(String(row.citations)) as { id: string, version: string, hash: string, locator: string }[]
      const sources = citations.map(source => ({ id: source.id, version: source.version, locator: source.locator, content: store.readBlob(source.hash).toString() }))
      // Each source has a content-addressed ID in the mail pipeline. Refuse ambiguous imported IDs
      // instead of silently citing a different version through the desktop's latest-version API.
      for (const source of sources) {
        if (Number(store.db.prepare('SELECT count(*) AS count FROM sources WHERE pod_id=? AND id=?').get(namespace, source.id)!.count) !== 1) throw new Error('Publication requires an unambiguous source version')
        const input = { expectedRevision: revision, checkpoint, sources: [source], claims: [] }
        if (Buffer.byteLength(JSON.stringify(input)) > 250 * 1024) throw new Error('Retained evidence exceeds the generic publication frame; review before publication')
        const saved = await context.progress.commit(input)
        if (saved.revision !== revision + 1) throw new Error('Progress publication returned an invalid revision')
        revision = saved.revision
      }
      const next = { ...checkpoint, externalMailKnowledge: { version: 1, row: Number(row.sequence), store: 'mail-knowledge/knowledge.sqlite' } }
      const reply = await context.progress.commit({ expectedRevision: revision, checkpoint: next, sources: [], claims: [{ id: String(row.id), matter: String(row.matter), kind: row.kind as 'finding' | 'question' | 'gap', text: String(row.body), sourceIds: citations.map(source => source.id), ...(row.supersedes ? { supersedes: String(row.supersedes) } : {}) }] })
      if (!Number.isSafeInteger(reply.revision) || reply.revision !== revision + 1) throw new Error('Progress publication returned an invalid revision')
      revision = reply.revision; checkpoint = next
    }
  }
}
