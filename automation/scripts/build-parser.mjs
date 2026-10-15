import { build } from 'tsup'
import { copyFileSync, readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
await build({ entry: { 'mail-parser': 'src/parsers/entry.ts' }, outDir: 'dist/parser', format: ['esm'], platform: 'node', target: 'node24', clean: true, splitting: false, noExternal: ['pdfjs-dist','html-to-text','fflate'], outExtension: () => ({ js: '.mjs' }) })
copyFileSync('node_modules/pdfjs-dist/legacy/build/pdf.worker.mjs','dist/parser/pdf.worker.mjs')
const sha = path => createHash('sha256').update(readFileSync(path)).digest('hex')
writeFileSync('dist/parser/manifest.json',JSON.stringify({version:1,entry:sha('dist/parser/mail-parser.mjs'),worker:sha('dist/parser/pdf.worker.mjs'),memoryMiB:192,timeMs:15000,outputBytes:256*1024},null,2)+'\n')
