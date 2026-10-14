<script lang="ts">
 import { api, apiErrorMessage } from '$api/client';
 import type { PDFDocumentProxy, PDFDocumentLoadingTask } from 'pdfjs-dist';
 import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url';

 let { path }: { path: string } = $props();
 let pdf = $state<PDFDocumentProxy | null>(null);
 let canvas = $state<HTMLCanvasElement>();
 let pageNumber = $state(1);
 let scale = $state(1.25);
 let loading = $state(true);
 let rendering = $state(false);
 let error = $state('');

 $effect(() => {
  const source = path;
  let cancelled = false;
  let task: PDFDocumentLoadingTask | undefined;
  pdf = null;
  pageNumber = 1;
  loading = true;
  error = '';
  async function load() {
   try {
    const [library, data] = await Promise.all([import('pdfjs-dist'), api.bytes(source)]);
    if (cancelled) return;
    library.GlobalWorkerOptions.workerSrc = workerUrl;
    task = library.getDocument({ data, cMapUrl: '/pdfjs/cmaps/', cMapPacked: true, standardFontDataUrl: '/pdfjs/standard_fonts/', wasmUrl: '/pdfjs/wasm/' });
    const document = await task.promise;
    if (!cancelled) pdf = document;
   } catch (err) {
    if (!cancelled) error = apiErrorMessage(err, 'Unable to preview this PDF.');
   } finally { if (!cancelled) loading = false; }
  }
  load();
  return () => { cancelled = true; task?.destroy(); };
 });

 $effect(() => {
  const document = pdf;
  const target = canvas;
  const number = pageNumber;
  const zoom = scale;
  if (!document || !target) return;
  let cancelled = false;
  let task: ReturnType<Awaited<ReturnType<PDFDocumentProxy['getPage']>>['render']> | undefined;
  rendering = true;
  async function render() {
   try {
    const page = await document!.getPage(number);
    if (cancelled) return;
    const viewport = page.getViewport({ scale: zoom });
    const ratio = window.devicePixelRatio || 1;
    target!.width = Math.ceil(viewport.width * ratio);
    target!.height = Math.ceil(viewport.height * ratio);
    target!.style.width = `${viewport.width}px`;
    target!.style.height = `${viewport.height}px`;
    task = page.render({ canvas: target!, viewport, transform: ratio === 1 ? undefined : [ratio, 0, 0, ratio, 0, 0] });
    await task.promise;
   } catch {
    if (!cancelled) error = 'Unable to render this PDF page.';
   } finally { if (!cancelled) rendering = false; }
  }
  render();
  return () => { cancelled = true; task?.cancel(); };
 });
</script>

<section class="pdf-viewer" aria-label="PDF preview" aria-busy={loading || rendering}>
 {#if loading}
  <p role="status">Loading PDF…</p>
 {:else if error}
  <p role="alert">{error}</p>
 {:else if pdf}
  <div class="toolbar">
   <button onclick={() => pageNumber--} disabled={pageNumber <= 1 || rendering}>Previous page</button>
   <span>Page {pageNumber} of {pdf.numPages}</span>
   <button onclick={() => pageNumber++} disabled={pageNumber >= pdf.numPages || rendering}>Next page</button>
   <label>Zoom <select bind:value={scale} disabled={rendering}>
    <option value={0.75}>75%</option><option value={1}>100%</option><option value={1.25}>125%</option><option value={1.5}>150%</option><option value={2}>200%</option>
   </select></label>
  </div>
  <div class="pages"><canvas bind:this={canvas} aria-label={`PDF page ${pageNumber}`}></canvas></div>
 {/if}
</section>

<style>
 .pdf-viewer { border: 1px solid var(--dd-border); margin: 1rem 0; }
 .toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 0.75rem; padding: 0.75rem; background: var(--dd-surface); }
 button, select { padding: 0.4rem 0.6rem; background: var(--dd-background); color: var(--dd-text); border: 1px solid var(--dd-border); }
 button { cursor: pointer; }
 button:disabled { opacity: 0.5; cursor: default; }
 .pages { overflow: auto; max-height: 80vh; padding: 1rem; background: #525252; }
 canvas { display: block; margin: auto; background: white; }
 p { padding: 1rem; }
</style>
