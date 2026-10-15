import { afterEach, expect, it } from 'vitest'
import { mkdtempSync, realpathSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { archiveHistory, importKnowledge } from '../src/migration.js'
import type { Snapshot } from '../src/migration.js'
import { MailStore, digest } from '../src/store.js'
import { knowledgePublisher } from '../src/publish.js'

const roots: string[] = []
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true }) })
const snapshot = (): Snapshot => ({ version: 1, schema: 52, pods: [{ id: 'pod', revision: 3 }], tables: Object.fromEntries(['checkpoints', 'sources', 'claims', 'mail_inventory', 'mail_items', 'mail_receipts', 'mail_contexts', 'source_derivations', 'action_approvals', 'effect_attempts', 'effect_receipts'].map(name => [name, []])), archives: [] })
it('preserves all approval states without converting old approval into authority', () => {
  const input = snapshot()
  const states = ['collecting', 'requested', 'approved', 'denied', 'expired', 'consumed', 'withdrawn', 'unknown']
  input.tables.action_approvals = states.map(state => ({ id: state, action: 'mail.archive', state, payload: JSON.stringify({ mailbox: 'Owner@example.com', messageId: 'message' }) }))
  input.tables.action_approvals.push({ id: 'program', action: 'program.run', state: 'approved', payload: '{}' })
  const history = archiveHistory(input)
  expect(history.map(row => row.state)).toEqual(states)
  expect(history.every(row => row.account === 'owner@example.com' && digest(row.payload) === row.payloadSha256)).toBe(true)
  expect(history.some(row => 'authorization' in row)).toBe(false)
})
it('uses current attempt outcomes and retains every earlier unknown receipt', () => {
  const input = snapshot()
  const states = ['intent', 'unknown', 'confirmed_applied', 'confirmed_not_applied']
  for (const state of states) {
    input.tables.effect_attempts.push({ action_key: state, attempt: 1, operation: 'mail.archive', state })
    for (const outcome of ['intent', 'unknown', state]) input.tables.effect_receipts.push({ action_key: state, attempt: 1, outcome, body: JSON.stringify({ mailbox: 'owner@example.com', messageId: state }) })
  }
  const records = archiveHistory(input)
  expect(records.map(row => row.state)).toEqual(['unknown', 'unknown', 'consumed', 'notApplied'])
  expect(records.every(row => JSON.parse(row.payload).receipts.length === 3)).toBe(true)
  input.tables.effect_receipts = []
  expect(() => archiveHistory(input)).toThrow('intent')
})
it('imports source content, citations, revisions and checkpoints into fresh standalone storage', () => {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'mail-migration-'))); roots.push(root)
  const source = join(root, 'source'); mkdirSync(source, { mode: 0o700 }); mkdirSync(join(source, 'blobs'))
  const input = snapshot(); const hash = digest('evidence')
  input.tables.sources.push({ pod_id: 'pod', id: 's', version: 'v1', locator: 'mail:test', hash })
  input.tables.checkpoints.push({ pod_id: 'pod', revision: 7, body: '{"mail":"retained"}' })
  input.tables.claims.push({ pod_id: 'pod', id: 'gap', matter: 'mail:test', kind: 'gap', body: 'Unexamined', citations: JSON.stringify([{ id: 's', version: 'v1', hash, locator: 'mail:test' }]), supersedes: null, revision: 7 })
  const raw = JSON.stringify(input); const expected = digest(raw)
  writeFileSync(join(source, 'snapshot.json'), raw); writeFileSync(join(source, 'manifest.json'), JSON.stringify({ version: 1, sha256: expected })); writeFileSync(join(source, 'blobs', hash), 'evidence')
  const destination = join(root, 'new')
  importKnowledge(source, expected, 'pod', destination)
  const store = new MailStore(destination)
  try { expect(store.getPod('pod').bindingRevision).toBe(3); expect(store.checkpoint('pod').revision).toBe(7); expect(store.knowledge('pod')[0].id).toBe('gap'); expect(store.readBlob(hash).toString()).toBe('evidence') } finally { store.close() }
  expect(readFileSync(join(source, 'snapshot.json')).toString()).toBe(raw)
  expect(() => importKnowledge(source, expected, 'pod', destination)).toThrow('fresh')
  writeFileSync(join(source, 'blobs', hash), 'changed')
  expect(() => importKnowledge(source, expected, 'pod', join(root, 'corrupt'))).toThrow('mismatch')
})
it('publishes external gaps and exact evidence through generic progress and resumes after interruption', async () => {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'mail-publication-'))); roots.push(root)
  const store = new MailStore(join(root, 'store')); store.createNamespace('pod')
  try {
    store.commitProgress({ podId: 'pod', expectedRevision: 0, checkpoint: {}, sources: [{ id: 's', version: 'v1', locator: 'mail:test', content: 'evidence' }], claims: [{ id: 'gap', kind: 'gap', matter: 'm', text: 'Unexamined', sourceIds: ['s'] }] })
    const calls: unknown[] = []; let saved = {}
    const publish = knowledgePublisher(store, 'pod', { input: { checkpointRevision: 4, checkpoint: {} }, progress: { commit: async input => { calls.push(input); saved = input.checkpoint; return { revision: input.expectedRevision + 1 } } } })
    await publish(); await publish()
    expect(calls).toHaveLength(2); expect(calls[0]).toMatchObject({ expectedRevision: 4, sources: [{ content: 'evidence' }], claims: [] }); expect(calls[1]).toMatchObject({ expectedRevision: 5, claims: [{ id: 'gap', kind: 'gap' }] })
    await knowledgePublisher(store, 'pod', { input: { checkpointRevision: 6, checkpoint: saved }, progress: { commit: async () => { throw new Error('Already published') } } })()
  } finally { store.close() }
})
