import { DatabaseSync } from 'node:sqlite'
import { copyFileSync, existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { MailStore, digest } from './store.js'

const knowledgeTables = ['checkpoints', 'sources', 'claims', 'mail_inventory', 'mail_items', 'mail_receipts', 'mail_contexts', 'source_derivations'] as const
const historyTables = ['approval_batches', 'action_approvals', 'effect_attempts', 'effect_receipts'] as const
type Row = Record<string, string | number | null>
export interface History { source: string, operationId: string, account: string, message: string, state: string, payload: string, payloadSha256: string }
export interface Snapshot { version: 1, schema: 52, pods: Row[], tables: Record<string, Row[]>, archives: { name: string, body: string }[] }
function regular(path: string): Buffer {
  if (!lstatSync(path).isFile() || realpathSync(path) !== path) throw new Error('Migration source must be a canonical regular file')
  return readFileSync(path)
}
function receipt(source: string, operationId: string, account: unknown, message: unknown, state: string, value: unknown): History {
  if (typeof account !== 'string' || !account.includes('@') || typeof message !== 'string' || !message) throw new Error('Legacy archive record has no unambiguous mailbox/message identity')
  const payload = JSON.stringify(value)
  return { source, operationId, account: account.toLowerCase(), message, state, payload, payloadSha256: digest(payload) }
}

/** No imported state authorizes execution. Uncertain/consumed history creates mailbox-wide replay holds. */
export function archiveHistory(snapshot: Snapshot): History[] {
  const records: History[] = []
  for (const row of snapshot.tables.action_approvals) {
    if (row.action !== 'mail.archive') continue
    const payload = JSON.parse(String(row.payload))
    records.push(receipt('pods/action_approvals', String(row.id), payload.mailbox, payload.messageId, String(row.state), row))
  }
  for (const attempt of snapshot.tables.effect_attempts) {
    if (attempt.operation !== 'mail.archive') continue
    const receipts = snapshot.tables.effect_receipts.filter(row => row.action_key === attempt.action_key && row.attempt === attempt.attempt)
    const intent = receipts.find(row => row.outcome === 'intent')
    if (!intent) throw new Error('Legacy archive attempt has no intent receipt')
    const identity = JSON.parse(String(intent.body))
    const state = attempt.state === 'confirmed_applied' ? 'consumed' : attempt.state === 'confirmed_not_applied' ? 'notApplied' : 'unknown'
    records.push(receipt('pods/effect_attempts', `${attempt.action_key}/${attempt.attempt}`, identity.mailbox, identity.messageId, state, { attempt, receipts }))
  }
  for (const file of snapshot.archives) {
    const value = JSON.parse(file.body)
    if (value.manifest?.version !== 1 || !Array.isArray(value.manifest.items) || !Array.isArray(value.outcomes)) throw new Error('Unsupported legacy archive manifest')
    for (const item of value.manifest.items) {
      const outcome = value.outcomes.find((row: { id: string }) => row.id === item.id)
      // A missing/skipped outcome in an executing/completed batch is not proof that no write occurred.
      const state = outcome?.state === 'archived' ? 'consumed' : outcome?.state === 'unknown' || ['executing', 'completed', 'unknown'].includes(value.state) ? 'unknown' : value.state === 'denied' ? 'denied' : value.state === 'expired' ? 'expired' : value.state === 'preparing' ? 'collecting' : value.state === 'pending' ? 'requested' : undefined
      if (!state) throw new Error('Unsupported legacy archive state')
      records.push(receipt(`pods/archive/${value.manifest.podId}`, `${value.manifest.id}/${digest(item.id)}`, value.manifest.mailbox, item.id, state, { file: file.name, record: value, item }))
    }
  }
  return records
}

/** Read-only rehearsal is safe while running. A rollout snapshot requires the desktop and all runs stopped. */
export function exportPodsHistory(profile: string, destination: string): { sha256: string, counts: Record<string, number>, accounts: { file: string, sha256: string, count: number }[] } {
  if (realpathSync(profile) !== profile || existsSync(destination)) throw new Error('Use a canonical profile and a fresh export directory')
  const database = join(profile, 'control.sqlite')
  if (!lstatSync(database).isFile() || realpathSync(database) !== database) throw new Error('Invalid source database')
  const db = new DatabaseSync(database, { readOnly: true })
  let snapshot: Snapshot
  try {
    db.exec('BEGIN')
    if (Number(db.prepare('PRAGMA user_version').get()!.user_version) !== 52) throw new Error('Migration supports only reviewed Pods schema 52')
    const tables: Record<string, Row[]> = {}
    for (const table of [...knowledgeTables, ...historyTables]) tables[table] = db.prepare(`SELECT * FROM ${table} ORDER BY rowid`).all() as Row[]
    snapshot = { version: 1, schema: 52, pods: db.prepare('SELECT id,revision FROM pods ORDER BY rowid').all() as Row[], tables, archives: [] }
    const archives = join(profile, 'mail-archive')
    if (existsSync(archives)) {
      for (const pod of readdirSync(archives).sort()) {
        if (!/^[a-f0-9-]{36}$/.test(pod)) throw new Error('Unexpected archive directory')
        for (const name of readdirSync(join(archives, pod)).sort()) {
          if (!/^[a-f0-9-]{36}\.json$/.test(name)) throw new Error('Unexpected archive entry; stop the desktop before exporting')
          const path = join(archives, pod, name); const before = lstatSync(path); const body = regular(path); const after = lstatSync(path)
          if (before.mtimeMs !== after.mtimeMs || before.ino !== after.ino || before.size !== after.size) throw new Error('Archive changed during export')
          snapshot.archives.push({ name: `${pod}/${name}`, body: body.toString() })
        }
      }
    }
    db.exec('COMMIT')
  } finally { db.close() }
  const history = archiveHistory(snapshot)
  mkdirSync(destination, { mode: 0o700 }); mkdirSync(join(destination, 'blobs'), { mode: 0o700 })
  for (const key of new Set(snapshot.tables.sources.map(row => String(row.hash)))) {
    if (!/^[a-f0-9]{64}$/.test(key)) throw new Error('Invalid legacy source hash')
    const raw = regular(join(profile, 'blobs', key))
    if (digest(raw) !== key) throw new Error('Legacy source content mismatch')
    writeFileSync(join(destination, 'blobs', key), raw, { flag: 'wx', mode: 0o600, flush: true })
  }
  const body = JSON.stringify(snapshot)
  writeFileSync(join(destination, 'snapshot.json'), body, { flag: 'wx', mode: 0o600, flush: true })
  const accounts = [...new Set(history.map(row => row.account))].sort().map(account => {
    const records = history.filter(row => row.account === account)
    const raw = JSON.stringify({ version: 1, records }); const file = `archive-${digest(account)}.json`
    if (Buffer.byteLength(raw) > 16 * 1024 * 1024 || records.length > 10000) throw new Error('Mailbox history exceeds import bound; do not activate migration')
    writeFileSync(join(destination, file), raw, { flag: 'wx', mode: 0o600, flush: true })
    return { file, sha256: digest(raw), count: records.length }
  })
  const result = { sha256: digest(body), counts: Object.fromEntries(Object.entries(snapshot.tables).map(([table, rows]) => [table, rows.length])), accounts }
  // Completion marker is written last. An interrupted export must never be imported.
  writeFileSync(join(destination, 'manifest.json'), JSON.stringify({ version: 1, ...result }), { flag: 'wx', mode: 0o600, flush: true })
  return result
}

/** Import into a fresh independent store only; the old database and blobs remain untouched. */
export function importKnowledge(source: string, expectedSha256: string, namespace: string, destination: string): void {
  if (existsSync(destination)) throw new Error('Knowledge migration requires a fresh destination')
  const manifest = JSON.parse(regular(join(source, 'manifest.json')).toString())
  const raw = regular(join(source, 'snapshot.json'))
  if (manifest.version !== 1 || manifest.sha256 !== expectedSha256 || digest(raw) !== expectedSha256) throw new Error('Snapshot digest mismatch or incomplete export')
  const snapshot = JSON.parse(raw.toString()) as Snapshot
  if (snapshot.version !== 1 || snapshot.schema !== 52) throw new Error('Unsupported knowledge snapshot')
  const pod = snapshot.pods.find(row => row.id === namespace)
  if (!pod) throw new Error('Unknown legacy namespace')
  const store = new MailStore(destination)
  try {
    store.createNamespace(namespace, Number(pod.revision))
    for (const row of snapshot.tables.sources.filter(row => row.pod_id === namespace)) {
      if (!/^[a-f0-9]{64}$/.test(String(row.hash))) throw new Error('Invalid source hash')
      const raw = regular(join(source, 'blobs', String(row.hash)))
      if (digest(raw) !== row.hash) throw new Error('Source content mismatch')
      store.putBlob(raw)
    }
    store.transaction(() => {
      store.db.prepare('DELETE FROM checkpoints WHERE pod_id=?').run(namespace)
      for (const table of knowledgeTables) {
        const columns = store.db.prepare(`PRAGMA table_info(${table})`).all().map(row => String(row.name))
        for (const row of snapshot.tables[table].filter(row => row.pod_id === namespace)) {
          if (columns.some(key => !(key in row)) || Object.keys(row).some(key => !columns.includes(key))) throw new Error('Knowledge schema changed')
          store.db.prepare(`INSERT INTO ${table} VALUES(${columns.map(() => '?').join(',')})`).run(...columns.map(key => row[key]))
        }
      }
    })
    copyFileSync(join(source, 'manifest.json'), join(destination, 'migration-receipt.json'))
  } finally { store.close() }
}
