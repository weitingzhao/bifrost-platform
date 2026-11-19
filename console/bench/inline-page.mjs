// Folds a built page (its one JS and one CSS) into a single file for an Artifact:
// no <!doctype>/<html>/<head>/<body> (the host wraps it), the title first, styles and the module inline.
//   node bench/inline-page.mjs <outDir>/bench/gen2-page.html <page.html>
import fs from 'node:fs'
import path from 'node:path'

const [src, dest] = process.argv.slice(2)
const html = fs.readFileSync(src, 'utf8')
const dir = path.dirname(src)
const read = ref => fs.readFileSync(path.resolve(dir, ref), 'utf8')
const title = html.match(/<title>[\s\S]*?<\/title>/)[0]
const ownStyle = (html.match(/<style>[\s\S]*?<\/style>/) ?? [''])[0]
// Font stylesheets from Google Fonts stay links (the only stylesheet host an Artifact allows).
const fonts = [...html.matchAll(/<link rel="stylesheet" href="https:\/\/fonts\.googleapis\.com[^"]*"[^>]*>/g)].map(m => m[0]).join('\n')
const css = [...html.matchAll(/<link rel="stylesheet"[^>]*href="([^"]+)"[^>]*>/g)].filter(m => !/^https?:/.test(m[1])).map(m => read(m[1])).join('\n')
const js = [...html.matchAll(/<script type="module"[^>]*src="([^"]+)"[^>]*><\/script>/g)].map(m => read(m[1])).join('\n')
// A literal </script> inside the bundle would end the inline block early.
const safe = js.replace(/<\/script/gi, '<\\/script')
fs.writeFileSync(dest, `${title}\n${fonts}\n${ownStyle}\n<style>${css}</style>\n<div id="root"></div>\n<script type="module">${safe}</script>\n`)
console.log(`${dest}: ${(fs.statSync(dest).size / 1e6).toFixed(2)} MB`)
