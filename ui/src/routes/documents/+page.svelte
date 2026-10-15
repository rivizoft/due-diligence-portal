<script lang="ts">
	import { formatFileSize } from '$lib/utils/format';
	import { type LocalizedText, t, locale, intlLocale, categoryName, number } from '$lib/i18n';
	import { onMount } from 'svelte';
	import type { Category, Document } from '$types/api';
	import { api, apiErrorMessage } from '$api/client';
 import { authStore } from '$stores/authStore.svelte';
 let canManage = $derived(authStore.isAdmin || authStore.isCompanyMember);
 let deleting = $state('');
 let actionError = $state<LocalizedText>('');
 let downloading = $state<Record<string, boolean>>({});

	const PAGE_SIZE = 50;

	let documents = $state<Document[]>([]);
	let categories = $state<Category[]>([]);
	let selectedCategory = $state('');
	let searchQuery = $state('');
	let searchMode = $state(false);
	let loading = $state(true);
	let loadError = $state('');
	let page = $state(1);
	let total = $state(0);

	let totalPages = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));

	async function loadDocuments() {
		loading = true;
		loadError = '';
		searchMode = false;
		try {
			const qs = new URLSearchParams();
			if (selectedCategory) qs.set('category_id', selectedCategory);
			qs.set('limit', String(PAGE_SIZE));
			qs.set('offset', String((page - 1) * PAGE_SIZE));
			const res = await api.get<Document[]>(`/documents?${qs.toString()}`);
			documents = res.data ?? [];
			total = res.meta?.total ?? documents.length;
		} catch {
			loadError = 'Failed to load documents.';
			documents = [];
			total = 0;
		}
		loading = false;
	}

	// The categories endpoint returns a nested tree; flatten it and keep only
	// categories that actually contain documents (so empty categories are not
	// offered as filters that always return nothing).
	function flattenCategories(tree: Category[], depth = 0): Category[] {
		const out: Category[] = [];
		for (const c of tree) {
			out.push({ ...c, name: `${'— '.repeat(depth)}${c.name}` });
			if (c.children?.length) out.push(...flattenCategories(c.children, depth + 1));
		}
		return out;
	}

	async function loadCategories() {
		try {
			const res = await api.get<Category[]>('/categories');
			categories = flattenCategories(res.data ?? []).filter((c) => (c.document_count ?? 0) > 0);
		} catch {
			categories = [];
		}
	}

	function onCategoryChange() {
		page = 1;
		searchQuery = '';
		loadDocuments();
	}

	function goToPage(p: number) {
		if (p < 1 || p > totalPages) return;
		page = p;
		loadDocuments();
	}

	async function handleSearch() {
		if (!searchQuery.trim()) {
			page = 1;
			await loadDocuments();
			return;
		}
		loading = true;
		loadError = '';
		searchMode = true;
		try {
			const res = await api.post<Document[]>('/documents/search', { query: searchQuery });
			documents = res.data ?? [];
			total = documents.length;
		} catch {
			loadError = 'Search failed.';
			documents = [];
		}
		loading = false;
	}

 async function downloadDoc(id: string, name: string) {
  if (downloading[id]) return; downloading[id] = true; actionError = '';
  try { await api.download(`/documents/${id}/download`, name); }
  catch (err) { actionError = apiErrorMessage(err, 'Download failed. Use Download to retry.'); }
  finally { downloading[id] = false; }
 }

 async function deleteDoc(doc: Document) {
  if (deleting || !canManage || !confirm($t('Delete "{name}" and remove access to all its versions?', {name:doc.name}))) return;
  deleting = doc.id;
  actionError = '';
  try {
   await api.delete(`/documents/${doc.id}`);
   if (documents.length === 1 && page > 1) page--;
   if (searchMode) await handleSearch(); else await loadDocuments();
   await loadCategories();
   if (selectedCategory && !categories.some(cat => cat.id === selectedCategory)) {
    selectedCategory = '';
    page = 1;
    await loadDocuments();
   }
  } catch (err) { actionError = apiErrorMessage(err, 'Failed to delete document.'); }
  finally { deleting = ''; }
 }

	function formatSize(bytes: number): string { return formatFileSize(bytes, $locale); }

	function formatDate(iso: string): string {
		return new Date(iso).toLocaleDateString(intlLocale($locale), {
			year: 'numeric', month: 'short', day: 'numeric'
		});
	}

	onMount(() => {
		loadCategories();
		loadDocuments();
	});
</script>

<div class="page">
	<header class="page-header">
		<h1>{$t("Documents")}</h1>
		{#if canManage}<a href="/documents/upload" class="btn-primary">{$t("Upload Document")}</a>{/if}
	</header>

	<div class="filters">
		<div class="search-bar">
			<input
				type="text"
				placeholder={$t("Search documents...")}
				bind:value={searchQuery}
				onkeydown={(e) => e.key === 'Enter' && handleSearch()}
			/>
			<button onclick={handleSearch}>{$t("Search")}</button>
		</div>

		<select bind:value={selectedCategory} onchange={onCategoryChange}>
			<option value="">{$t("All Categories")}</option>
			{#each categories as cat}
				<option value={cat.id}>{categoryName(cat, $locale)} ({$number(cat.document_count ?? 0)})</option>
			{/each}
		</select>
	</div>

 {#if actionError}<p role="alert">{$t(actionError)}</p>{/if}
	{#if loading}
		<div class="loading">{$t("Loading documents...")}</div>
	{:else if loadError}
		<div class="empty">{$t(loadError)} <button class="link" onclick={loadDocuments}>{$t("Retry")}</button></div>
	{:else if documents.length === 0}
		<div class="empty">{$t("No documents found.")}</div>
	{:else}
		<table>
			<thead>
				<tr>
					<th>{$t("Name")}</th>
					<th>{$t("Category")}</th>
					<th>{$t("Size")}</th>
					<th>{$t("Version")}</th>
					<th>{$t("Uploaded")}</th>
					<th>{$t("Actions")}</th>
				</tr>
			</thead>
			<tbody>
				{#each documents as doc}
					<tr>
						<td>
							<a href="/documents/{doc.id}">{doc.name}</a>
							{#if doc.description}
								<span class="description">{doc.description}</span>
							{/if}
						</td>
						<td>{categoryName({id:doc.category_id, name:doc.category_name ?? ''}, $locale)}</td>
						<td>{formatSize(doc.file_size)}</td>
						<td>{$t("v")}{$number(doc.current_version)}</td>
						<td>{formatDate(doc.created_at)}</td>
						<td>
							<a class="btn-small" href="/documents/{doc.id}">{$t("View")}</a>
 {#if doc.mime_type === 'application/pdf'}<a class="btn-small" href="/documents/{doc.id}/preview" target="_blank" rel="noopener">{$t("Open PDF")}</a>{/if}
       {#if doc.can_download}<button class="btn-small" onclick={() => downloadDoc(doc.id, doc.name)} disabled={downloading[doc.id]} aria-busy={downloading[doc.id] ?? false}>{#if downloading[doc.id]}<span class="spinner" aria-hidden="true"></span>{$t("Preparing…")}{:else}{$t("Download")}{/if}</button>{/if}
       {#if canManage}<button class="btn-small" onclick={() => deleteDoc(doc)} disabled={!!deleting}>{$t("Delete document")}</button>{/if}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
			{#if !searchMode && totalPages > 1}
				<div class="pagination">
					<button class="page-btn" onclick={() => goToPage(page - 1)} disabled={page <= 1}>{$t("Previous")}</button>
					<span class="page-info">{$t('Page {page} of {total}', {page,total:totalPages})} · {$t('{count} documents', {count:total})}</span>
					<button class="page-btn" onclick={() => goToPage(page + 1)} disabled={page >= totalPages}>{$t("Next")}</button>
				</div>
			{/if}
	{/if}
</div>

<style>
 .spinner {display:inline-block;width:.75rem;height:.75rem;margin-right:.4rem;border:2px solid var(--dd-border);border-top-color:var(--dd-primary);border-radius:50%;animation:spin .8s linear infinite;}
 @keyframes spin {to{transform:rotate(360deg);}}
 @media(prefers-reduced-motion:reduce){.spinner{animation:none;}}

	.pagination {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 1rem;
		margin-top: 1.5rem;
	}
	.page-btn {
		padding: 0.5rem 1rem;
		background: var(--dd-surface);
		border: 1px solid var(--dd-border);
		color: var(--dd-text);
		cursor: pointer;
		font-size: 0.8125rem;
	}
	.page-btn:disabled { opacity: 0.4; cursor: default; }
	.page-info { font-size: 0.8125rem; color: var(--dd-text-secondary); }
	.page {
		max-width: 1200px;
		margin: 0 auto;
		padding: 2rem;
	}

	.page-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 2rem;
	}

	h1 { font-weight: 400; font-size: 1.75rem; }

	.btn-primary {
		padding: 0.625rem 1.25rem;
		background: var(--dd-primary);
		color: #fff;
		border: none;
		text-decoration: none;
		font-size: 0.875rem;
	}

	.filters {
		display: flex;
		gap: 1rem;
		margin-bottom: 1.5rem;
	}

	.search-bar {
		display: flex;
		flex: 1;
		gap: 0.5rem;
	}

	.search-bar input {
		flex: 1;
		padding: 0.625rem 1rem;
		background: var(--dd-surface);
		border: 1px solid var(--dd-border);
		color: var(--dd-text);
		font-size: 0.875rem;
	}

	.search-bar button, select {
		padding: 0.625rem 1rem;
		background: var(--dd-surface);
		border: 1px solid var(--dd-border);
		color: var(--dd-text);
		cursor: pointer;
	}

	table {
		width: 100%;
		border-collapse: collapse;
	}

	th {
		text-align: left;
		padding: 0.75rem 1rem;
		background: var(--dd-surface);
		border-bottom: 1px solid var(--dd-border);
		font-size: 0.75rem;
		text-transform: uppercase;
		color: var(--dd-text-secondary);
	}

	td {
		padding: 0.75rem 1rem;
		border-bottom: 1px solid var(--dd-border);
		font-size: 0.875rem;
	}

	td a { color: var(--dd-primary); }

	.description {
		display: block;
		font-size: 0.75rem;
		color: var(--dd-text-secondary);
		margin-top: 0.25rem;
	}

	.btn-small {
		padding: 0.25rem 0.75rem;
		background: var(--dd-surface);
		border: 1px solid var(--dd-border);
		color: var(--dd-text);
		text-decoration: none;
		font-size: 0.75rem;
		cursor: pointer;
	}

	.link {
		background: none;
		border: none;
		color: var(--dd-primary);
		cursor: pointer;
		text-decoration: underline;
		font-size: inherit;
	}

	.loading, .empty {
		text-align: center;
		padding: 3rem;
		color: var(--dd-text-secondary);
	}
</style>
