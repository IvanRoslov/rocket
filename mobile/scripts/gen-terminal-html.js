#!/usr/bin/env node
// Generates src/terminal/terminalHtml.ts — a self-contained xterm.js page
// for the terminal WebView. Rerun after upgrading @xterm/*:
//   npm run gen:terminal
const fs = require('fs')
const path = require('path')

const root = path.join(__dirname, '..')
const read = (p) => fs.readFileSync(path.join(root, 'node_modules', p), 'utf8')

const xtermJs = read('@xterm/xterm/lib/xterm.js')
const fitJs = read('@xterm/addon-fit/lib/addon-fit.js')
const xtermCss = read('@xterm/xterm/css/xterm.css')

const page = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no">
<style>
${xtermCss}
html,body{margin:0;padding:0;height:100%;background:#161618;overflow:hidden}
#term{position:absolute;inset:0;padding:8px}
</style>
</head>
<body>
<div id="term"></div>
<script>${xtermJs}</script>
<script>${fitJs}</script>
<script>
(function(){
  var term = new Terminal({
    fontSize: 12,
    fontFamily: 'Menlo, monospace',
    cursorBlink: true,
    scrollback: 3000,
    theme: {
      background: '#161618',
      foreground: '#d4d4d8',
      cursor: '#16a34a',
      selectionBackground: 'rgba(79,70,229,.35)'
    }
  });
  var fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById('term'));
  fit.fit();

  var ws = null;
  var pingTimer = null;
  var encoder = new TextEncoder();

  function post(msg){
    if (window.ReactNativeWebView) window.ReactNativeWebView.postMessage(JSON.stringify(msg));
  }

  function sendResize(){
    if (ws && ws.readyState === 1) ws.send(JSON.stringify({type:'resize', cols: term.cols, rows: term.rows}));
  }

  function connect(url){
    if (ws) { try { ws.close(); } catch(e){} ws = null; }
    if (pingTimer) { clearInterval(pingTimer); pingTimer = null; }
    ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';
    ws.onopen = function(){
      post({type:'status', value:'open'});
      fit.fit();
      sendResize();
      pingTimer = setInterval(function(){
        if (ws && ws.readyState === 1) ws.send(JSON.stringify({type:'ping'}));
      }, 30000);
    };
    ws.onmessage = function(e){
      if (typeof e.data === 'string') return; // no text frames expected from server
      term.write(new Uint8Array(e.data));
    };
    ws.onclose = function(){ post({type:'status', value:'closed'}); };
    ws.onerror = function(){ post({type:'status', value:'error'}); };
  }

  term.onData(function(d){
    if (ws && ws.readyState === 1) ws.send(encoder.encode(d));
  });

  var resizeTimer = null;
  window.addEventListener('resize', function(){
    if (resizeTimer) clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function(){ fit.fit(); sendResize(); }, 150);
  });

  window.rocketTerm = {
    connect: connect,
    sendKey: function(seq){
      if (ws && ws.readyState === 1) ws.send(encoder.encode(seq));
    },
    disconnect: function(){ if (ws) { try { ws.close(); } catch(e){} } }
  };
  post({type:'ready'});
})();
</script>
</body>
</html>
`

const out = `// GENERATED FILE — do not edit by hand. Regenerate with: npm run gen:terminal
// Self-contained xterm.js page for the terminal WebView (see scripts/gen-terminal-html.js).
export const TERMINAL_HTML: string = ${JSON.stringify(page)}
`

const outPath = path.join(root, 'src', 'terminal', 'terminalHtml.ts')
fs.mkdirSync(path.dirname(outPath), { recursive: true })
fs.writeFileSync(outPath, out)
console.log(`wrote ${outPath} (${(out.length / 1024).toFixed(0)} KiB)`)
