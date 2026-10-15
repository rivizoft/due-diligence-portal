<script lang="ts">
	import { type LocalizedText, t } from '$lib/i18n';
 import { onMount } from 'svelte';
 import { api, apiErrorMessage } from '$api/client';
 import type { PdfPageInfo } from '$types/api';
 let { documentId, version, pages }: { documentId: string; version: number; pages: PdfPageInfo[] } = $props();
 type PageState = { url: string; loading: boolean; error: LocalizedText };
 let states = $state<Record<number, PageState>>({});
 let current = $state(1);
 let container: HTMLDivElement;
 const controllers = new Map<number, AbortController>();
 const urls = new Map<number, string>();
 let mounted = false;
 async function load(number: number) {
  if (!mounted || number < 1 || number > pages.length || states[number]) return;
  const controller = new AbortController(); controllers.set(number, controller);
  states[number] = { url: '', loading: true, error: '' };
  try {
   const response = await api.binary(`/documents/${encodeURIComponent(documentId)}/versions/${version}/preview-pages/${number}`, { signal: controller.signal });
   if (!mounted || controller.signal.aborted) return;
   if (response.blob.type !== 'image/png') throw new Error('This page could not be displayed.');
   const url = URL.createObjectURL(response.blob); urls.set(number, url);
   states[number] = { url, loading: false, error: '' };
  } catch (err) {
   if (mounted && !controller.signal.aborted) states[number] = { url: '', loading: false, error: apiErrorMessage(err, 'Page preparation failed. Please try again.') };
  } finally { if (controllers.get(number) === controller) controllers.delete(number); }
 }
 function activate(number: number) {
  if (!mounted) return;
  current = number;
  for (const key of Object.keys(states).map(Number)) {
   if (key < number - 1 || key > number + 2) {
    controllers.get(key)?.abort(); controllers.delete(key);
    const url = urls.get(key); if (url) URL.revokeObjectURL(url); urls.delete(key); delete states[key];
   }
  }
  void load(number); void load(number + 1);
 }
 function jump(number: number) {
  if (number < 1 || number > pages.length) return;
  activate(number); container.querySelector<HTMLElement>(`[data-page="${number}"]`)?.scrollIntoView({ block: 'start' });
 }
 function retry(number: number) { delete states[number]; void load(number); }
 onMount(() => {
  mounted = true; activate(1);
  const visible = new Set<number>();
  const observer = new IntersectionObserver(entries => {
   for (const entry of entries) {
    const n = Number((entry.target as HTMLElement).dataset.page);
    if (entry.isIntersecting) visible.add(n); else visible.delete(n);
   }
   if (visible.size) activate(Math.min(...visible));
  }, { threshold: 0.05 });
  container.querySelectorAll('[data-page]').forEach(element => observer.observe(element));
  return () => { mounted = false; observer.disconnect(); controllers.forEach(c => c.abort()); controllers.clear(); urls.forEach(url => URL.revokeObjectURL(url)); urls.clear(); };
 });
</script>
<div class="image-preview" bind:this={container}>
 <nav aria-label={$t("PDF page navigation")}>
  <button onclick={() => jump(current - 1)} disabled={current === 1} aria-label={$t("Previous page")}>{$t("Previous")}</button>
  <span aria-live="polite">{$t('Page {page} of {total}', {page:current,total:pages.length})}</span>
  <button onclick={() => jump(current + 1)} disabled={current === pages.length} aria-label={$t("Next page")}>{$t("Next")}</button>
 </nav>
 {#each pages as page, index}
  {@const number = index + 1}
  <section data-page={number} aria-label={$t('Page {page}', {page:number})} style:aspect-ratio={`${page.width} / ${page.height}`}>
   {#if states[number]?.url}
    <img src={states[number].url} alt={$t('Page {page}', {page:number})} />
   {:else if states[number]?.error}
    <div class="page-status" role="alert"><p>{$t(states[number].error)}</p><button onclick={() => retry(number)}>{$t('Retry page {page}', {page:number})}</button></div>
   {:else}
    <p class="page-status" role="status"><span class="spinner" aria-hidden="true"></span>{states[number]?.loading ? $t('Preparing page {page}…', {page:number}) : $t('Page {page}', {page:number})}</p>
   {/if}
  </section>
 {/each}
</div>
<style>
 .image-preview {max-width:760px;margin:auto;}
 nav {position:sticky;top:0;z-index:1;display:flex;justify-content:space-between;align-items:center;gap:1rem;background:var(--dd-surface);padding:.75rem;border:1px solid var(--dd-border);}
 section {background:white;border:1px solid var(--dd-border);margin:1rem 0;scroll-margin-top:5rem;position:relative;}
 img {display:block;width:100%;height:100%;object-fit:contain;}
 .page-status {position:absolute;inset:0;display:flex;flex-direction:column;justify-content:center;align-items:center;gap:.75rem;padding:1rem;color:#444;text-align:center;}
 button {padding:.5rem .75rem;border:1px solid var(--dd-border);background:var(--dd-surface);color:var(--dd-text);cursor:pointer;}
 button:disabled{opacity:.5;cursor:default;}
 .spinner{width:1rem;height:1rem;border:2px solid #ddd;border-top-color:var(--dd-primary);border-radius:50%;animation:spin .8s linear infinite;}
 @keyframes spin{to{transform:rotate(360deg);}}
 @media(prefers-reduced-motion:reduce){.spinner{animation:none;}}
</style>
