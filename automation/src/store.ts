import { DatabaseSync } from 'node:sqlite'
import { createHash, randomUUID } from 'node:crypto'
import { chmodSync, closeSync, existsSync, fsyncSync, lstatSync, mkdirSync, openSync, readdirSync, readFileSync, realpathSync, renameSync, statfsSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

export const digest = (content: string | Buffer): string => createHash('sha256').update(content).digest('hex')
function text(value: unknown, label: string, maximum = 4096): asserts value is string { if (typeof value !== 'string' || !value || value.length > maximum || value.includes('\0')) throw new Error(`Invalid ${label}`) }
function hash(value: string) { if (!/^[a-f0-9]{64}$/.test(value)) throw new Error('Invalid content digest') }
function syncDirectory(path: string) { const fd = openSync(path, 'r'); try { fsyncSync(fd) } finally { closeSync(fd) } }
export interface SourceInput { id: string, locator: string, version: string, content: string }
export interface ClaimInput { id: string, matter: string, kind: 'finding' | 'question' | 'gap', text: string, sourceIds: string[], supersedes?: string }
export interface ProgressInput {
  podId: string
  expectedRevision: number
  checkpoint: Record<string, unknown>
  sources: SourceInput[]
  claims: ClaimInput[]
}
export type CommitPoint = 'staged' | 'renamed' | 'beforeCommit' | 'committed'

/** Standalone mail knowledge storage. It never opens the Pods control database. */
export class MailStore {
  readonly db: DatabaseSync
  readonly blobs: string
  constructor(readonly root: string, readonly maximumBytes = 1024 * 1024 * 1024) {
    mkdirSync(root, { recursive: true, mode: 0o700 })
    if (realpathSync(root) !== root || !lstatSync(root).isDirectory() || (lstatSync(root).mode & 0o077)) throw new Error('Mail storage requires a private canonical directory')
    this.blobs = join(root, 'blobs'); mkdirSync(this.blobs, { recursive: true, mode: 0o700 })
    if (realpathSync(this.blobs) !== this.blobs) throw new Error('Mail blobs cannot use symbolic links')
    const database = join(root, 'knowledge.sqlite')
    if (existsSync(database) && !lstatSync(database).isFile()) throw new Error('Mail database must be a regular file')
    this.db = new DatabaseSync(database)
    chmodSync(database, 0o600)
    this.db.exec('PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000')
    const version = this.db.prepare('PRAGMA user_version').get()!.user_version
    if (version === 0) this.db.exec(`BEGIN IMMEDIATE; CREATE TABLE namespaces(id TEXT PRIMARY KEY,binding_revision INTEGER NOT NULL); CREATE TABLE data_settings(id INTEGER PRIMARY KEY,used_bytes INTEGER NOT NULL); INSERT INTO data_settings VALUES(1,0); CREATE TABLE checkpoints(pod_id TEXT PRIMARY KEY REFERENCES namespaces(id), revision INTEGER NOT NULL, body TEXT NOT NULL);
CREATE TABLE sources(
  pod_id TEXT NOT NULL REFERENCES namespaces(id),
  id TEXT NOT NULL,
  version TEXT NOT NULL,
  locator TEXT NOT NULL,
  hash TEXT NOT NULL,
  PRIMARY KEY(pod_id,id,version)
);
CREATE TABLE claims(
  pod_id TEXT NOT NULL REFERENCES namespaces(id),
  id TEXT NOT NULL,
  matter TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('finding','question','gap')),
  body TEXT NOT NULL,
  citations TEXT NOT NULL,
  supersedes TEXT,
  revision INTEGER NOT NULL,
  PRIMARY KEY(pod_id,id)
);
CREATE TABLE mail_inventory(
  pod_id TEXT PRIMARY KEY REFERENCES namespaces(id),
  scope TEXT NOT NULL,
  phase TEXT NOT NULL,
  folder_index INTEGER NOT NULL,
  cursor TEXT,
  completed_at INTEGER
);
CREATE TABLE mail_items(
  pod_id TEXT NOT NULL REFERENCES namespaces(id),
  account TEXT NOT NULL,
  id TEXT NOT NULL,
  folder TEXT NOT NULL,
  source_id TEXT NOT NULL,
  conversation TEXT NOT NULL,
  metadata TEXT NOT NULL,
  PRIMARY KEY(pod_id,account,id)
);
CREATE TABLE mail_receipts(
  pod_id TEXT NOT NULL REFERENCES namespaces(id),
  source_id TEXT NOT NULL,
  recipe TEXT NOT NULL,
  context_hash TEXT NOT NULL,
  revision INTEGER NOT NULL,
  PRIMARY KEY(pod_id,source_id,recipe)
);
CREATE TABLE mail_contexts(
  pod_id TEXT NOT NULL REFERENCES namespaces(id),
  hash TEXT NOT NULL,
  body TEXT NOT NULL,
  PRIMARY KEY(pod_id,hash)
);
CREATE TABLE source_derivations(
  pod_id TEXT NOT NULL REFERENCES namespaces(id),
  source_id TEXT NOT NULL,
  original_id TEXT NOT NULL,
  operation TEXT NOT NULL,
  PRIMARY KEY(pod_id,source_id)
); PRAGMA user_version=1; COMMIT;`)
    else if (version !== 1) { this.db.close(); throw new Error('Unsupported mail knowledge schema') }
  }
  close(): void { this.db.close() }
  createNamespace(id: string, bindingRevision = 1): void {
    text(id, 'namespace'); if (!Number.isSafeInteger(bindingRevision) || bindingRevision < 1) throw new Error('Invalid binding revision')
    this.transaction(() => { this.db.prepare('INSERT OR IGNORE INTO namespaces VALUES(?,?)').run(id,bindingRevision); this.db.prepare("INSERT OR IGNORE INTO checkpoints VALUES(?,0,'{}')").run(id); if (this.getPod(id).bindingRevision > bindingRevision) throw new Error('Stale mail namespace binding'); this.db.prepare('UPDATE namespaces SET binding_revision=? WHERE id=?').run(bindingRevision,id) })
  }
  getPod(id: string): { bindingRevision: number } { const row = this.db.prepare('SELECT binding_revision FROM namespaces WHERE id=?').get(id); if (!row) throw new Error('Unknown mail namespace'); return { bindingRevision: Number(row.binding_revision) } }
  transaction<T>(work: () => T): T { this.db.exec('BEGIN IMMEDIATE'); try { const result = work(); this.db.exec('COMMIT'); return result } catch (error) { this.db.exec('ROLLBACK'); throw error } }
  assertStorage(additionalBytes: number): void {
    // Include retained raw sources and interrupted staging files in the bound.
    let used = 0; for (const name of ['knowledge.sqlite', 'knowledge.sqlite-wal', 'knowledge.sqlite-shm']) { const path = join(this.root, name); if (existsSync(path)) used += lstatSync(path).size }
    for (const name of readdirSync(this.blobs)) { const info = lstatSync(join(this.blobs,name)); if (!info.isFile()) throw new Error('Unexpected mail blob entry'); used += info.size }
    const disk = statfsSync(this.root)
    if (used + additionalBytes >= this.maximumBytes || disk.bavail * disk.bsize - additionalBytes < 256 * 1024 * 1024) throw new Error('Mail storage limit reached')
  }
  putBlob(content: string | Buffer, observe: (point: CommitPoint) => void = () => {}): string {
    const key = digest(content); const target = join(this.blobs, key)
    if (existsSync(target)) {
      if (digest(readFileSync(target)) !== key) throw new Error('Corrupt stored blob')
      return key
    }
    this.assertStorage(Buffer.byteLength(content))
    const stage = join(this.blobs, `.stage-${randomUUID()}`)
    const fd = openSync(stage, 'wx', 0o600)
    try { writeFileSync(fd, content); fsyncSync(fd) }
    finally { closeSync(fd) }
    observe('staged')
    renameSync(stage, target); syncDirectory(this.blobs); this.db.prepare('UPDATE data_settings SET used_bytes=used_bytes+? WHERE id=1').run(Buffer.byteLength(content)); observe('renamed')
    return key
  }

  readBlob(key: string): Buffer {
    hash(key)
    const content = readFileSync(join(this.blobs, key))
    if (digest(content) !== key) throw new Error('Corrupt stored blob')
    return content
  }

  checkpoint(podId: string): { revision: number, body: Record<string, unknown> } {
    const row = this.db.prepare('SELECT * FROM checkpoints WHERE pod_id=?').get(podId)
    if (!row) throw new Error('Pod checkpoint not found')
    return { revision: row.revision as number, body: JSON.parse(row.body as string) }
  }

  commitProgress(input: ProgressInput, observe: (point: CommitPoint) => void = () => {}, publish: (revision: number) => void = () => {}): number {
    this.getPod(input.podId)
    if (JSON.stringify(input).length > 1024 * 1024) throw new Error('Progress exceeds frame limit')
    const sources = input.sources.map((source) => {
      text(source.id, 'source ID'); text(source.version, 'source version'); text(source.locator, 'source locator'); text(source.content, 'source content', 1024 * 1024)
      return { ...source, hash: this.putBlob(source.content, observe) }
    })
    const revision = this.transaction(() => {
      if (this.checkpoint(input.podId).revision !== input.expectedRevision) throw new Error('Stale checkpoint revision')
      for (const source of sources) {
        const prior = this.db.prepare('SELECT hash,locator FROM sources WHERE pod_id=? AND id=? AND version=?').get(input.podId, source.id, source.version)
        if (prior && (prior.hash !== source.hash || prior.locator !== source.locator)) throw new Error('Source version conflict')
        this.db.prepare('INSERT OR IGNORE INTO sources VALUES(?,?,?,?,?)').run(input.podId, source.id, source.version, source.locator, source.hash)
      }
      for (const claim of input.claims) this.insertClaim(input.podId, input.expectedRevision + 1, claim)
      this.db.prepare('UPDATE checkpoints SET revision=revision+1,body=? WHERE pod_id=?').run(JSON.stringify(input.checkpoint), input.podId)
      publish(input.expectedRevision + 1)
      observe('beforeCommit')
      return input.expectedRevision + 1
    })
    observe('committed')
    return revision
  }

  private insertClaim(podId: string, revision: number, claim: ClaimInput): void {
    text(claim.id, 'claim ID'); text(claim.matter, 'matter'); text(claim.text, 'claim')
    if (!['finding', 'question', 'gap'].includes(claim.kind) || !Array.isArray(claim.sourceIds) || !claim.sourceIds.length) throw new Error('Claim needs sources and a valid kind')
    const citations = claim.sourceIds.map((id) => {
      const row = this.db.prepare('SELECT id,version,hash,locator FROM sources WHERE pod_id=? AND id=? ORDER BY rowid DESC LIMIT 1').get(podId, id)
      if (!row) throw new Error('Claim references unavailable evidence')
      return row
    })
    if (claim.supersedes && !this.db.prepare('SELECT id FROM claims WHERE pod_id=? AND id=? AND matter=?').get(podId, claim.supersedes, claim.matter)) throw new Error('Superseded claim not found in matter')
    const prior = this.db.prepare('SELECT * FROM claims WHERE pod_id=? AND id=?').get(podId, claim.id)
    const values = [podId, claim.id, claim.matter, claim.kind, claim.text, JSON.stringify(citations), claim.supersedes ?? null, revision]
    if (prior) {
      if (prior.body !== claim.text || prior.citations !== JSON.stringify(citations) || prior.kind !== claim.kind || prior.matter !== claim.matter || prior.supersedes !== (claim.supersedes ?? null)) throw new Error('Immutable claim conflict')
      return
    }
    this.db.prepare('INSERT INTO claims VALUES(?,?,?,?,?,?,?,?)').run(...values)
  }
  knowledge(id: string): Record<string, unknown>[] { this.getPod(id); return this.db.prepare('SELECT * FROM claims WHERE pod_id=? ORDER BY revision,rowid').all(id).map(row => ({ ...row, citations: JSON.parse(String(row.citations)) })) }

}
