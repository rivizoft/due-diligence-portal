<script lang="ts">
	import { t } from '$lib/i18n';
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';

	let { children }: { children: Snippet } = $props();
	let currentPath = $derived(page.url.pathname);
</script>

<div class="admin-layout">
	<nav class="admin-nav">
		<h2>{$t("Admin")}</h2>
		<ul>
			<li><a href="/admin/users" class:active={currentPath.startsWith('/admin/users')}>{$t("Users")}</a></li>
			<li><a href="/admin/permissions" class:active={currentPath.startsWith('/admin/permissions')}>{$t("Access Grants")}</a></li>
			<li><a href="/admin/categories" class:active={currentPath.startsWith('/admin/categories')}>{$t("Categories")}</a></li>
			<li><a href="/admin/branding" class:active={currentPath.startsWith('/admin/branding')}>{$t("Branding")}</a></li>
			<li><a href="/admin/watermark" class:active={currentPath.startsWith('/admin/watermark')}>{$t("Watermark")}</a></li>
			<li><a href="/admin/storage" class:active={currentPath.startsWith('/admin/storage')}>{$t("Storage")}</a></li>
			<li><a href="/admin/nda" class:active={currentPath.startsWith('/admin/nda')}>{$t("NDA Templates")}</a></li>
			<li><a href="/activity" class:active={currentPath.startsWith('/activity')}>{$t("Activity Log")}</a></li>
		</ul>
	</nav>

	<main class="admin-content">
		{@render children()}
	</main>
</div>

<style>
	.admin-layout {
		display: flex;
		min-height: 0;
	}

	.admin-nav {
		width: 240px;
		background: var(--dd-sidebar);
		border-right: 1px solid var(--dd-border);
		padding: 1.5rem 0;
		flex-shrink: 0;
	}

	.admin-nav h2 {
		font-size: 0.875rem;
		font-weight: 600;
		text-transform: uppercase;
		color: var(--dd-text-secondary);
		padding: 0 1.5rem;
		margin-bottom: 1rem;
	}

	.admin-nav ul {
		list-style: none;
		padding: 0;
		margin: 0;
	}

	.admin-nav li a {
		display: block;
		padding: 0.625rem 1.5rem;
		color: var(--dd-text);
		text-decoration: none;
		font-size: 0.875rem;
		border-left: 3px solid transparent;
	}

	.admin-nav li a:hover {
		background: var(--dd-hover);
		border-left-color: var(--dd-primary);
	}

	.admin-nav li a.active {
		background: var(--dd-hover);
		border-left-color: var(--dd-primary);
		color: var(--dd-text);
	}

	.admin-content {
		flex: 1;
		min-width: 0;
		overflow-x: auto;
		padding: 2rem;
		max-width: 1000px;
	}
	@media (max-width: 800px) {
		.admin-layout { flex-direction: column; }
		.admin-nav { width: 100%; padding: 0.75rem 0; border-right: 0; border-bottom: 1px solid var(--dd-border); }
		.admin-nav ul { display: flex; flex-wrap: wrap; }
		.admin-nav li a { padding: 0.5rem 0.75rem; }
		.admin-content { padding: 1rem; }
	}
</style>
