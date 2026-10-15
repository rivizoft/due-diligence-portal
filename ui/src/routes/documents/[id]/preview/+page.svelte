<script lang="ts">
	import { type LocalizedText, t, hasTranslation } from '$lib/i18n';
 import { page } from '$app/state';
 import { api, apiErrorMessage } from '$api/client';
 import PdfPreview from '$components/PdfPreview.svelte';
 import type { PdfPreviewInfo } from '$types/api';
 let info = $state<PdfPreviewInfo | null>(null);
 let loading = $state(true);
 let error = $state<LocalizedText>('');
 let objectUrl = $state('');
 let filename = $state('document.pdf');
 let attempt = $state(0);
 const documentId = $derived(page.params.id ?? '');
 const version = $derived(page.url.searchParams.get('version'));
 $effect(() => {
  const id = documentId; const requested = version; const retry = attempt;
  const controller = new AbortController(); let url = '';
  info = null; loading = true; error = ''; objectUrl = '';
  async function prepare() {
   try {
    if (requested !== null && !/^[1-9]\d*$/.test(requested)) throw new Error('Invalid document version.');
    const base = `/documents/${encodeURIComponent(id)}${requested ? `/versions/${requested}` : ''}`;
    const response = await api.get<PdfPreviewInfo>(`${base}/preview-info`, { signal: controller.signal });
    if (!response.data) throw new Error('PDF preview is unavailable.');
    if (controller.signal.aborted) return;
    info = response.data; filename = response.data.name ?? 'document.pdf';
    if (info.mode === 'pdf') {
     const result = await api.binary(`/documents/${encodeURIComponent(id)}/versions/${info.version_number}/preview`, { signal: controller.signal });
     if (controller.signal.aborted) return;
     if (result.blob.type !== 'application/pdf') throw new Error('PDF preview is unavailable.');
     url = URL.createObjectURL(result.blob); objectUrl = url; filename = result.filename ?? filename;
    } else if (info.mode !== 'images' || !info.pages.length) throw new Error('PDF preview is unavailable.');
   } catch (err) {
    if (!controller.signal.aborted) error = apiErrorMessage(err, err instanceof Error && hasTranslation(err.message) ? err.message : 'PDF preparation failed. Please try again.');
   } finally { if (!controller.signal.aborted) loading = false; }
  }
  void retry; void prepare();
  return () => { controller.abort(); if (url) URL.revokeObjectURL(url); };
 });
</script>
<svelte:head><title>{info?.name ?? $t("PDF preview")}</title></svelte:head>
<main class="preview-shell">
 <header><h1>{info?.name ?? $t("PDF preview")}</h1><a href="/documents/{documentId}">{$t("Document details")}</a></header>
 {#if loading}
  <p class="status" role="status"><span class="spinner" aria-hidden="true"></span>{$t("Preparing PDF…")}</p>
 {:else if error}
  <div class="error" role="alert"><p>{$t(error)}</p><button onclick={() => attempt++}>{$t("Retry")}</button></div>
 {:else if info?.mode === 'pdf' && objectUrl}
  <p class="fallback">{$t("If the browser cannot display this PDF,")} <a href={objectUrl} download={filename} role="button">{$t("Download PDF")}</a>.</p>
  <iframe title={$t("PDF preview")} src={objectUrl}></iframe>
 {:else if info?.mode === 'images'}
  {#key `${documentId}:${info.version_number}:${attempt}`}
   <PdfPreview {documentId} version={info.version_number} pages={info.pages} />
  {/key}
 {/if}
</main>
<style>
 .preview-shell {max-width:1200px;margin:auto;padding:1.5rem;}
 header {display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-bottom:1rem;}
 h1 {font-size:1.25rem;font-weight:400;overflow-wrap:anywhere;}
 iframe {width:100%;height:calc(100vh - 180px);min-height:500px;border:1px solid var(--dd-border);}
 .status {display:flex;align-items:center;gap:.75rem;padding:2rem;}
 .error {padding:1rem;border:1px solid var(--dd-danger,#c00);}
 button {padding:.6rem 1rem;background:var(--dd-primary);color:white;border:0;cursor:pointer;}
 .fallback {font-size:.875rem;margin-bottom:.75rem;}
 .spinner {width:1rem;height:1rem;border:2px solid var(--dd-border);border-top-color:var(--dd-primary);border-radius:50%;animation:spin .8s linear infinite;}
 @keyframes spin {to {transform:rotate(360deg);}}
 @media(prefers-reduced-motion:reduce){.spinner{animation:none;}}
</style>
