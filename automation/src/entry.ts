import { runMailKnowledge } from './run.js'
import type { RecipeContext } from './run.js'
import { parseMailScope } from './contract.js'

/** Separately delivered recipe source. It uses only the generic Pods script contract. */
export async function run(context: RecipeContext & { variables: Record<string, string> }) {
 const v=context.variables
 const scope=parseMailScope({account:v.mailbox,folders:JSON.parse(v.folders??'[]'),attachments:v.attachments==='on',...(v.since?{since:v.since}:{})})
 for(const key of ['mail-application','parser-application','parser-worker','parser-worker-sha256'])if(!v[key])throw new Error(`Configure ${key} before validating the external recipe`)
 return runMailKnowledge(context,{scope,application:v['mail-application']!,parserApplication:v['parser-application']!,parserWorker:v['parser-worker']!,parserWorkerSha256:v['parser-worker-sha256']!})
}
