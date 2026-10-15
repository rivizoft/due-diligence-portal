<script lang="ts">
	import { t, initializeLocale } from '$lib/i18n';
	import { browser } from '$app/environment';
	import LanguageSwitcher from '$components/LanguageSwitcher.svelte';
	import '../app.css';
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { authStore } from '$stores/authStore.svelte';
	import { api } from '$api/client';
	import NavHeader from '$components/NavHeader.svelte';

	if (browser) initializeLocale();

	let { children }: { children: Snippet } = $props();

	// Routes reachable without authentication.
	const publicRoutes = ['/', '/login', '/register', '/unauthorized'];
	// Routes reachable while authenticated but before the NDA is signed.
	const ndaExemptPrefixes = ['/nda-sign'];

	let isPublicRoute = $derived(publicRoutes.includes(page.url.pathname));
	let showNav = $derived(authStore.isAuthenticated && !isPublicRoute);
	// Gate rendering of protected pages until the guard has run, to avoid flashing
	// protected UI to unauthenticated users.
	let ready = $state(false);

	async function guard(pathname: string) {
		authStore.loadFromStorage();
		if (authStore.token && !authStore.user) {
			await authStore.restore();
		}

		const isPublic = publicRoutes.includes(pathname);
		if (isPublic) {
			ready = true;
			return;
		}

		if (!authStore.isAuthenticated) {
			await goto('/login');
			return;
		}

		// Admin area requires the admin role.
		if (pathname.startsWith('/admin') && !authStore.isAdmin) {
			await goto('/unauthorized');
			return;
		}

		// NDA gate: investors must sign the active NDA before accessing the data room.
		if (authStore.isInvestor && !ndaExemptPrefixes.some((p) => pathname.startsWith(p))) {
			try {
				const res = await api.get<{ signed: boolean; template_id?: string }>('/nda/status');
				if (res.data && res.data.signed === false) {
					await goto('/nda-sign/required');
					return;
				}
			} catch {
				// If the status check fails, fail open to avoid locking users out on a
				// transient error; the backend still enforces per-document access.
			}
		}

		ready = true;
	}

	// Re-run the guard whenever the path changes.
	$effect(() => {
		ready = false;
		guard(page.url.pathname);
	});
</script>

<svelte:head>
	<title>{$t("Due Diligence Portal")}</title>
</svelte:head>

<div class="app-shell">
{#if showNav}
	<NavHeader />
{/if}

<div class="app-content">
	{#if ready}
		{@render children()}
	{:else if !isPublicRoute}
		<div class="route-loading">{$t("Loading…")}</div>
	{/if}
</div>
<LanguageSwitcher />
</div>

<style>
	.app-shell {
		display: flex;
		flex-direction: column;
		min-height: 100dvh;
	}
	.app-content {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-width: 0;
	}
	.app-content > :global(*) { flex: 1; }

	.route-loading {
		display: flex;
		justify-content: center;
		align-items: center;
		min-height: 60vh;
		color: var(--dd-text-secondary);
	}
</style>
