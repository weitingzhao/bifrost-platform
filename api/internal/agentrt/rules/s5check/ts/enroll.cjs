'use strict';

// Offline core-maintainer tool. Never invoked by the candidate checker or CI.
const fs=require('node:fs');
const path=require('node:path');
const {createContext}=require('./compiler.cjs');
const {enroll}=require('./contracts.cjs');
function readTree(root){
  const files={};
  function visit(dir){
    for(const entry of fs.readdirSync(path.join(root,dir),{withFileTypes:true})){
      const p=dir?dir+'/'+entry.name:entry.name;
      if(p.startsWith('api/internal/agentrt/rules/s5check/'))continue;
      if(entry.isSymbolicLink())throw new Error(`Symlink is not an enrolled tree file: ${p}`);
      if(entry.isDirectory()){if(!entry.name.startsWith('.')&&!['node_modules','dist'].includes(entry.name))visit(p);}
      else if(entry.isFile()&&/\.(?:ts|tsx|json)$/.test(p))files[p]={content:fs.readFileSync(path.join(root,p),'utf8'),mode:'100644'};
    }
  }
  visit('');return files;
}
if(require.main===module){
  if(process.argv.length!==4)throw new Error('Usage: node enroll.cjs <reviewed-tree-root> <output-manifest>');
  const manifest=enroll(createContext(readTree(path.resolve(process.argv[2]))));
  fs.writeFileSync(process.argv[3],JSON.stringify(manifest,null,2)+'\n');
}
module.exports={readTree};
