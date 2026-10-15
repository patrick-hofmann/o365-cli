import { lstat, mkdir, readFile, realpath, rm, writeFile } from 'node:fs/promises'
import { randomUUID } from 'node:crypto'
import { dirname, join } from 'node:path'
import { MailStore, digest } from './store.js'
import { MailRecipeSession, mailToolRequest } from './recipe.js'
import { ingestMailPage } from './ingestion.js'
import type { MailScope } from './contract.js'
import { knowledgePublisher } from './publish.js'
import type { ProgressContext } from './publish.js'
import type { Extraction } from './parsers/extract.js'

interface ToolResult { exitCode: number, stdout: string, stderr: string, stdoutArtifact?: { path: string, sha256: string, bytes: number } }
export interface RecipeContext extends ProgressContext {
  signal?: AbortSignal
  input: { podId: string, assignmentRevision: number, workspace: string, eventIds: string[], checkpointRevision: number, checkpoint: Record<string, unknown>, validation?: boolean }
  tools: { invoke: (request: { application: string, argv: string[], output: 'artifact', timeoutMs?: number, isolation?: 'temporary', inputFiles?: {path:string,sha256:string}[] }) => Promise<ToolResult> }
  agent: { run: (request: { prompt: string, tools: [] }) => Promise<{ response: string }> }
  log: (text: string) => void
}
export interface MailRecipeOptions { scope: MailScope, application: string, parserApplication: string, parserWorker: string, parserWorkerSha256: string }

async function output(result: ToolResult, workspace: string, maximum: number): Promise<Buffer> {
  if (result.exitCode !== 0) throw new Error(`Mail tool failed (${result.exitCode}): ${result.stderr.slice(0,2000)}`)
  const artifact = result.stdoutArtifact
  if (!artifact || dirname(artifact.path) !== workspace || !/\/\.program-output-[a-f0-9-]{36}\.json$/.test(artifact.path) || artifact.bytes > maximum || !/^[a-f0-9]{64}$/.test(artifact.sha256)) throw new Error('Mail tool output is outside the assigned workspace or bound')
  const info = await lstat(artifact.path)
  if (!info.isFile() || info.isSymbolicLink() || info.size !== artifact.bytes || await realpath(artifact.path) !== artifact.path) throw new Error('Mail output artifact changed')
  const raw = await readFile(artifact.path)
  if (raw.length !== artifact.bytes || digest(raw) !== artifact.sha256) throw new Error('Mail output digest mismatch')
  await rm(artifact.path)
  return raw
}

/** Install this recipe as an external dependency; the desktop contains no provider or analysis behavior. */
export async function runMailKnowledge(context: RecipeContext, options: MailRecipeOptions) {
  const current = () => context.signal?.throwIfAborted()
  current()
  const gaps = new Set<string>()
  const result = (summary: string) => ({ status: gaps.size ? 'completedWithGaps' : 'completed', summary, completedInputIds: context.input.eventIds, gapIds: [...gaps].slice(0,1000) })
  // Contract validation never reads a mailbox or fabricates source evidence.
  if (context.input.validation) return result('Mail recipe contract is ready; provider work requires a live run')
  const workspace = await realpath(context.input.workspace)
  const root = join(workspace, 'mail-knowledge'); await mkdir(root,{ recursive:true,mode:0o700 })
  const store = new MailStore(root)
  const namespace = context.input.podId
  try {
    store.createNamespace(namespace,context.input.assignmentRevision)
    const publish = knowledgePublisher(store, namespace, context)
    await publish()
    const session = new MailRecipeSession(store,namespace,options.scope,async request => {
      const argv = mailToolRequest(options.scope,request).argv.slice(1)
      const raw = await output(await context.tools.invoke({application:options.application,argv,output:'artifact'}),workspace,32*1024*1024)
      const path = join(root,`mail-${randomUUID()}.json`); await writeFile(path,raw,{flag:'wx',mode:0o400})
      return ingestMailPage(store,namespace,root,{path,hash:digest(raw)},options.scope,request,current)
    },async source => {
      const input = join(root,`parse-${randomUUID()}.json`); await writeFile(input,source,{flag:'wx',mode:0o400})
      try {
        const raw = await output(await context.tools.invoke({application:options.parserApplication,argv:[input,digest(source),options.parserWorker,options.parserWorkerSha256],output:'artifact',timeoutMs:15000,isolation:'temporary',inputFiles:[{path:input,sha256:digest(source)}]}),workspace,256*1024)
        const value = JSON.parse(raw.toString()) as Extraction
        if (typeof value.text!=='string' || value.text.length>60000 || (value.gap!==null && (typeof value.gap!=='string' || value.gap.length>300)) || typeof value.parser!=='string') throw new Error('Invalid parser result')
        return value
      } catch { current(); return {text:'',gap:'Parser failed or exceeded its process limit; source is unexamined',parser:options.parserWorkerSha256} }
      finally { await rm(input,{force:true}) }
    },current)
    for(let unit=0;unit<20;unit++) {
      const next=await session.next() as {type:string,prompt:string,hash:string,foldersComplete:number,folders:number}
      if(next.type==='done')return result('Mail inventory and supported knowledge are up to date')
      if(next.type==='inventory'){context.log(`Mail inventory: ${next.foldersComplete}/${next.folders} folders`);continue}
      const answer=await context.agent.run({prompt:next.prompt,tools:[]})
      const committed=session.commit({hash:next.hash,response:answer.response})
      await publish()
      for(const id of committed.gapIds)gaps.add(id)
      context.log(`Committed sourced mail knowledge revision ${committed.revision}`)
    }
    return result('Bounded mail batch committed; remaining work continues on the next run')
  } finally {store.close()}
}
