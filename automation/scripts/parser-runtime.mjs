import { chmodSync, readFileSync, realpathSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
const root = realpathSync(resolve(dirname(fileURLToPath(import.meta.url)), '../dist/parser'))
const node = realpathSync(process.execPath)
const quote = value => `'${value.replaceAll("'", "'\\''")}'`
const entry = join(root, 'mail-parser.mjs')
const launcher = join(root, 'o365-extract')
writeFileSync(launcher, `#!/bin/sh\nexec ${quote(node)} --max-old-space-size=192 ${quote(entry)} "$@"\n`, {mode:0o700})
chmodSync(launcher,0o700)
writeFileSync(join(root,'runtime.json'), JSON.stringify({version:1,executable:node,arguments:['--max-old-space-size=192',entry],readDirectories:[root],environment:{}},null,2)+'\n',{mode:0o600})
console.log(JSON.stringify({application:launcher,runtime:join(root,'runtime.json'),workerPath:join(root,'pdf.worker.mjs'),...JSON.parse(readFileSync(join(root,'manifest.json'),'utf8'))},null,2))
