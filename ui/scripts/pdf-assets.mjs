// Ship all PDF.js rendering assets locally for disconnected deployments.
import { cpSync, mkdirSync } from 'node:fs';
for (const directory of ['cmaps', 'standard_fonts', 'wasm']) {
 mkdirSync('static/pdfjs', { recursive: true });
 cpSync(`node_modules/pdfjs-dist/${directory}`, `static/pdfjs/${directory}`, { recursive: true });
}
