import { readFile, lstat, realpath } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { extract } from './extract.js'
const digest = (bytes:Buffer) => createHash('sha256').update(bytes).digest('hex')
async function pinned(path:string,hash:string,maximum:number):Promise<Buffer>{
 const info=await lstat(path)
 if(!/^[a-f0-9]{64}$/.test(hash)||!info.isFile()||info.isSymbolicLink()||info.size>maximum||await realpath(path)!==path)throw new Error('Invalid parser input binding')
 const bytes=await readFile(path);if(bytes.length>maximum||digest(bytes)!==hash)throw new Error('Parser input changed');return bytes
}
async function main(){
 if(process.argv.length!==6)throw new Error('Expected source path/hash and worker path/hash')
 const source=JSON.parse((await pinned(process.argv[2]!,process.argv[3]!,32*1024*1024)).toString()) as {data:{body?:{contentType:string,content:string},contentBytes?:string,contentType?:string}}
 await pinned(process.argv[4]!,process.argv[5]!,16*1024*1024)
 const data=source.data;let bytes:Uint8Array;let type:string
 if(data.body){bytes=Buffer.from(data.body.content);type=data.body.contentType.toLowerCase()}
 else {if(typeof data.contentBytes!=='string'||data.contentBytes.length>28*1024*1024||/[^a-z0-9+/=]/i.test(data.contentBytes))throw new Error('Invalid attachment bytes');bytes=Buffer.from(data.contentBytes,'base64');if(Buffer.from(bytes).toString('base64')!==data.contentBytes)throw new Error('Invalid attachment encoding');type=data.contentType??''}
 const result=await extract(bytes,type,process.argv[4]!)
 const output=JSON.stringify(result);if(Buffer.byteLength(output)>256*1024)throw new Error('Parser output exceeded its bound')
 process.stdout.write(output)
}
void main().catch(()=>{console.error('Document parsing failed; source is unexamined');process.exitCode=1})
