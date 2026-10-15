<script lang="ts">
	import { formatFileSize } from '$lib/utils/format';
	import { type LocalizedText, t, locale, intlLocale, categoryName, number } from '$lib/i18n';
	import type { Document, DocumentVersion } from '$types/api';
	import { api, apiErrorMessage } from '$api/client';
 import { goto } from '$app/navigation';
	import { page } from '$app/state';

	let doc = $state<Document | null>(null);
	let versions = $state<DocumentVersion[]>([]);
	let loading = $state(true);
	let error = $state<LocalizedText>('');
	let downloadError = $state<LocalizedText>('');
 let downloading = $state<Record<string, boolean>>({});
 let canDownload = $state(false);
 let canManage = $state(false);
 let busy = $state(false);
 let file = $state<File | null>(null);
 let fileInput = $state<HTMLInputElement>();
 let changeNote = $state('');
 let actionError = $state<LocalizedText>('');
 let selectedVersion = $state(0);
 let previewVersion = $derived(versions.find(v => v.version_number === selectedVersion));

	let docId = $derived(page.params.id ?? '');

	async function load(id: string) {
		loading = true;
		error = '';
		try {
			const res = await api.get<{ document: Document; versions: DocumentVersion[]; permissions: { can_download: boolean; can_manage: boolean } }>(`/documents/${id}`);
			doc = res.data?.document ?? null;
			versions = res.data?.versions ?? [];
   canDownload = res.data?.permissions?.can_download ?? false;
   canManage = res.data?.permissions?.can_manage ?? false;
   selectedVersion = doc?.current_version ?? 0;
			// Record a view event (best-effort; failure must not block the page).
			api.post('/analytics/view-event', { document_id: id }).catch(() => {});
		} catch {
			error = 'Document not found or you do not have access.';
			doc = null;
		}
		loading = false;
	}

 async function downloadCurrent() { await download('current', `/documents/${docId}/download`, doc?.name ?? 'document'); }
 async function downloadVersion(v: number) { await download(String(v), `/documents/${docId}/versions/${v}`, `${doc?.name ?? 'document'}-v${v}`); }
 async function download(key: string, path: string, name: string) {
  if (downloading[key]) return; downloading[key] = true; downloadError = '';
  try { await api.download(path, name); }
  catch (err) { downloadError = apiErrorMessage(err, 'Download failed. Use Download to retry.'); }
  finally { downloading[key] = false; }
 }

 async function uploadVersion(event: SubmitEvent) {
  event.preventDefault();
  if (!file || busy || !canManage) return;
  busy = true;
  actionError = '';
  const id = docId;
  try {
   const form = new FormData();
   form.append('file', file);
   form.append('change_note', changeNote);
   await api.upload(`/documents/${id}/versions`, form);
   file = null;
   changeNote = '';
   if (fileInput) fileInput.value = '';
   await load(id);
  } catch (err) { actionError = apiErrorMessage(err, 'Failed to upload a new version.'); }
  finally { busy = false; }
 }

 async function deleteVersion(number: number) {
  if (busy || !canManage || !confirm($t('Delete version {version}? This cannot be undone.', {version:number}))) return;
  busy = true;
  actionError = '';
  const id = docId;
  try {
   await api.delete(`/documents/${id}/versions/${number}`);
   await load(id);
  } catch (err) { actionError = apiErrorMessage(err, 'Failed to delete version.'); }
  finally { busy = false; }
 }

 async function deleteDocument() {
  if (busy || !canManage || !confirm($t('Delete "{name}" and remove access to all its versions?', {name:doc?.name ?? ''}))) return;
  busy = true;
  actionError = '';
  try {
   await api.delete(`/documents/${docId}`);
   await goto('/documents');
  } catch (err) { actionError = apiErrorMessage(err, 'Failed to delete document.'); }
  finally { busy = false; }
 }

	function formatSize(bytes: number): string { return formatFileSize(bytes, $locale); }

	function formatDate(iso: string): string {
		return new Date(iso).toLocaleString(intlLocale($locale), {
			year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
		});
	}

	$effect(() => {
		if (docId) load(docId);
	});
</script>

<div class="page">
	<a href="/documents" class="back">{$t("← Back to Documents")}</a>

	{#if loading}
		<p class="loading">{$t("Loading…")}</p>
	{:else if error}
		<p class="empty">{$t(error)}</p>
	{:else if doc}
		<header class="doc-header">
			<h1>{doc.name}</h1>
			<div class="header-actions">
    {#if canDownload}<button class="btn-primary" onclick={downloadCurrent} disabled={busy || downloading.current} aria-busy={downloading.current ?? false}>{#if downloading.current}<span class="spinner" aria-hidden="true"></span>{$t("Preparing…")}{:else}{$t("Download")}{/if}</button>{/if}
    {#if canManage}<button class="btn-small danger" onclick={deleteDocument} disabled={busy}>{$t("Delete document")}</button>{/if}
   </div>
		</header>

		{#if actionError}<div class="error-msg" role="alert">{$t(actionError)}</div>{/if}
  {#if downloadError}<div class="error-msg">{$t(downloadError)}</div>{/if}
		{#if doc.description}<p class="description">{doc.description}</p>{/if}

		<dl class="meta">
			<div><dt>{$t("Category")}</dt><dd>{categoryName({id:doc.category_id, name:doc.category_name ?? '—'}, $locale)}</dd></div>
			<div><dt>{$t("Current version")}</dt><dd>{$t("v")}{$number(doc.current_version)}</dd></div>
			<div><dt>{$t("Size")}</dt><dd>{formatSize(doc.file_size)}</dd></div>
			<div><dt>{$t("Type")}</dt><dd>{doc.mime_type}</dd></div>
			<div><dt>{$t("Uploaded by")}</dt><dd>{doc.uploader_name ?? '—'}</dd></div>
			<div><dt>{$t("Uploaded")}</dt><dd>{formatDate(doc.created_at)}</dd></div>
			{#if doc.tags}<div><dt>{$t("Tags")}</dt><dd>{doc.tags}</dd></div>{/if}
		</dl>

  {#if previewVersion?.mime_type === 'application/pdf'}
   <h2>{$t("PDF preview — v")}{$number(selectedVersion)}</h2>
   <a class="btn-small" href="/documents/{docId}/preview?version={selectedVersion}" target="_blank" rel="noopener">{$t("Open PDF")}</a>
  {:else}
   <p class="description">{$t("Preview is available for PDF files only.")}</p>
  {/if}

  {#if canManage}
   <h2>{$t("Upload new version")}</h2>
   <form class="version-form" onsubmit={uploadVersion}>
    <label>{$t("Updated file")} <input type="file" bind:this={fileInput} onchange={(event) => file = event.currentTarget.files?.[0] ?? null} required disabled={busy} /></label>
    <label>{$t("Change note")} <input type="text" bind:value={changeNote} disabled={busy} /></label>
    <button class="btn-primary" type="submit" disabled={busy || !file}>{busy ? $t("Saving…") : $t("Upload new version")}</button>
   </form>
  {/if}

		<h2>{$t("Version history")}</h2>
		{#if versions.length === 0}
			<p class="empty">{$t("No versions recorded.")}</p>
		{:else}
			<table>
				<thead>
					<tr><th>{$t("Version")}</th><th>{$t("Size")}</th><th>{$t("Change note")}</th><th>{$t("Date")}</th><th></th></tr>
				</thead>
				<tbody>
					{#each versions as v}
						<tr>
							<td>{$t("v")}{$number(v.version_number)}{v.version_number === doc.current_version ? $t(" (current)") : ''}</td>
							<td>{formatSize(v.file_size)}</td>
							<td>{v.change_note ?? '—'}</td>
							<td>{formatDate(v.created_at)}</td>
							<td class="version-actions">
        {#if v.mime_type === 'application/pdf'}<button class="btn-small" onclick={() => selectedVersion = v.version_number} disabled={busy}>{$t("View")}</button>{/if}
        {#if canDownload}<button class="btn-small" onclick={() => downloadVersion(v.version_number)} disabled={busy || downloading[String(v.version_number)]} aria-busy={downloading[String(v.version_number)] ?? false}>{#if downloading[String(v.version_number)]}<span class="spinner" aria-hidden="true"></span>{$t("Preparing…")}{:else}{$t("Download")}{/if}</button>{/if}
        {#if canManage}<button class="btn-small danger" onclick={() => deleteVersion(v.version_number)} disabled={busy || versions.length <= 1} title={versions.length <= 1 ? $t("The last version cannot be deleted. Delete the document instead.") : $t("Delete this version")}>{$t("Delete version")}</button>{/if}
       </td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	{/if}
</div>

<style>
 .spinner {display:inline-block;width:.75rem;height:.75rem;margin-right:.4rem;border:2px solid var(--dd-border);border-top-color:var(--dd-primary);border-radius:50%;animation:spin .8s linear infinite;}
 @keyframes spin {to{transform:rotate(360deg);}}
 @media(prefers-reduced-motion:reduce){.spinner{animation:none;}}

 .header-actions, .version-actions { display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: center; }
 .danger { color: var(--dd-error, #da1e28); }
 .version-form { display: flex; flex-direction: column; gap: 1rem; padding: 1rem; border: 1px solid var(--dd-border); }
 .version-form label { display: flex; flex-direction: column; gap: 0.5rem; }
 .version-form input { background: var(--dd-surface); color: var(--dd-text); border: 1px solid var(--dd-border); padding: 0.5rem; }
 button:disabled { opacity: 0.5; cursor: default; }

	.page { max-width: 900px; margin: 0 auto; padding: 2rem; }
	.back { color: var(--dd-primary); text-decoration: none; font-size: 0.875rem; }
	.doc-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin: 1rem 0;
	}
	h1 { font-weight: 400; font-size: 1.75rem; }
	h2 { font-weight: 400; font-size: 1.25rem; margin: 2rem 0 1rem; }
	.description { color: var(--dd-text-secondary); margin-bottom: 1.5rem; }
	.error-msg {
		padding: 0.5rem 1rem;
		background: #da1e2815;
		border: 1px solid var(--dd-error, #da1e28);
		color: var(--dd-error, #da1e28);
		font-size: 0.875rem;
		margin-bottom: 1rem;
	}
	.meta {
		display: grid;
		grid-template-columns: repeat(2, 1fr);
		gap: 0.75rem 2rem;
		margin: 0;
	}
	.meta dt { font-size: 0.75rem; text-transform: uppercase; color: var(--dd-text-secondary); }
	.meta dd { margin: 0.125rem 0 0; font-size: 0.875rem; }
	table { width: 100%; border-collapse: collapse; }
	th {
		text-align: left;
		padding: 0.75rem 1rem;
		background: var(--dd-surface);
		border-bottom: 1px solid var(--dd-border);
		font-size: 0.75rem;
		text-transform: uppercase;
		color: var(--dd-text-secondary);
	}
	td { padding: 0.75rem 1rem; border-bottom: 1px solid var(--dd-border); font-size: 0.875rem; }
	.btn-primary {
		padding: 0.625rem 1.25rem;
		background: var(--dd-primary);
		color: #fff;
		border: none;
		cursor: pointer;
		font-size: 0.875rem;
	}
	.btn-small {
		padding: 0.25rem 0.75rem;
		background: var(--dd-surface);
		border: 1px solid var(--dd-border);
		color: var(--dd-text);
		cursor: pointer;
		font-size: 0.75rem;
	}
	.loading, .empty { color: var(--dd-text-secondary); text-align: center; padding: 2rem; }
</style>
