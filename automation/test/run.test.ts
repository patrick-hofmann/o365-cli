import { afterEach, expect, it } from 'vitest'
import { randomUUID } from 'node:crypto'
import { mkdtempSync, realpathSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { runMailKnowledge } from '../src/run.js'
import type { RecipeContext } from '../src/run.js'
import { digest, MailStore } from '../src/store.js'
const roots: string[] = []
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive:true,force:true }) })
it('runs the external recipe through generic tools, model and progress; resumes without duplicate knowledge', async () => {
 const workspace=realpathSync(mkdtempSync(join(tmpdir(),'external-mail-run-')));roots.push(workspace)
 const published=new MailStore(join(workspace,'desktop-fixture'));published.createNamespace('pod')
 let reads=0, models=0, parses=0
 const context:RecipeContext={input:{podId:'pod',assignmentRevision:1,workspace,eventIds:[],checkpointRevision:0,checkpoint:{}},log:()=>{},progress:{commit:async input=>({revision:published.commitProgress({...input,podId:'pod'})})},tools:{invoke:async request=>{
  expect(request.output).toBe('artifact')
  let data:unknown
  if(request.application==='mail'){
   reads++
   data={version:1,operation:'messages',account:'synthetic@example.invalid',folder:'rules',complete:true,items:[{id:'m1',changeKey:'v1',parentFolderId:'rules',conversationId:'c1',body:{contentType:'text',content:'Delivery is June 8.'}}]}
  }else{
   parses++;expect(request).toMatchObject({application:'parser',isolation:'temporary',timeoutMs:15000})
   const raw=readFileSync(request.argv[0]!);expect(digest(raw)).toBe(request.argv[1]);expect(request.inputFiles).toEqual([{path:request.argv[0],sha256:request.argv[1]}])
   data={text:JSON.parse(raw.toString()).data.body.content,gap:null,parser:'fixture-v1'}
  }
  const body=JSON.stringify(data);const path=join(workspace,`.program-output-${randomUUID()}.json`);writeFileSync(path,body,{mode:0o400})
  return {exitCode:0,stdout:'',stderr:'',stdoutArtifact:{path,sha256:digest(body),bytes:Buffer.byteLength(body)}}
 }},agent:{run:async request=>{
  models++;expect(request.tools).toEqual([])
  const evidence=JSON.parse(request.prompt.split('\nUNTRUSTED_CONTEXT\n')[1]!).evidence[0]
  return {response:JSON.stringify({claims:[{kind:'finding',text:'Delivery is June 8.',evidence:[{sourceId:evidence.id,quote:'Delivery is June 8.'}]}]})}
 }}}
 const options={scope:{account:'synthetic@example.invalid',folders:['rules'],attachments:false},application:'mail',parserApplication:'parser',parserWorker:'/assigned/parser-worker.mjs',parserWorkerSha256:'a'.repeat(64)}
 try {
  expect(await runMailKnowledge(context,options)).toMatchObject({status:'completed'})
  const checkpoint=published.checkpoint('pod');context.input.checkpointRevision=checkpoint.revision;context.input.checkpoint=checkpoint.body
  expect(await runMailKnowledge(context,options)).toMatchObject({status:'completed'})
  expect(published.knowledge('pod')).toHaveLength(1);expect(models).toBe(1);expect(parses).toBe(1);expect(reads).toBe(2)
  const citation=(published.knowledge('pod')[0]!.citations as {hash:string}[])[0]!
  expect(published.readBlob(citation.hash).toString()).toBe('Delivery is June 8.')
 }finally{published.close()}
})
