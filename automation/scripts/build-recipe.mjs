import { build } from 'tsup'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
await build({ entry:['src/entry.ts'], outDir:'dist/recipe', format:['esm'], target:'node24', bundle:true, splitting:false, sourcemap:false, minify:false, clean:true, outExtension:()=>({js:'.mjs'}) })
const bytes=readFileSync('dist/recipe/entry.mjs')
writeFileSync('dist/recipe/manifest.json',JSON.stringify({version:1,name:'mail-knowledge',source:'entry.mjs',sha256:createHash('sha256').update(bytes).digest('hex'),bytes:bytes.length,contract:'generic-pods-script',requires:['tools.invoke','agent.run','progress.commit']},null,2)+'\n')
