import { exportPodsHistory, importKnowledge } from '../dist/migration.js'
const [command,...args]=process.argv.slice(2)
if(command==='export'&&args.length===2)console.log(JSON.stringify(exportPodsHistory(...args),null,2))
else if(command==='import-knowledge'&&args.length===4){importKnowledge(...args);console.log(JSON.stringify({imported:true,activated:false}))}
else throw new Error('Usage: migrate.mjs export CANONICAL_PROFILE FRESH_EXPORT | import-knowledge EXPORT SHA256 NAMESPACE FRESH_DESTINATION')
