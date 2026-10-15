<script lang="ts">
	import { t } from '$lib/i18n';
	import { authStore } from '$stores/authStore.svelte';
</script>

<main>
	<div class="box">
		<h1>{$t("Access Denied")}</h1>
		<p>{$t("You don't have permission to view this page.")}</p>
		<a href={authStore.isAuthenticated ? '/documents' : '/login'} class="btn-primary">
			{authStore.isAuthenticated ? $t("Back to Documents") : $t("Sign In")}
		</a>
	</div>
</main>

<style>
	main { display: flex; justify-content: center; align-items: center; min-height: 0; }
	.box {
		text-align: center;
		max-width: 420px;
		padding: 2.5rem;
		background: var(--dd-surface);
		border: 1px solid var(--dd-border);
	}
	h1 { font-size: 1.75rem; font-weight: 400; margin-bottom: 1rem; }
	p { color: var(--dd-text-secondary); margin-bottom: 2rem; }
	.btn-primary {
		display: inline-block;
		padding: 0.75rem 1.5rem;
		background: var(--dd-primary);
		color: #fff;
		text-decoration: none;
		font-size: 0.875rem;
	}
</style>
