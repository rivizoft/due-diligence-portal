<script lang="ts">
	import { type LocalizedText, t } from '$lib/i18n';
	import { onMount } from 'svelte';
	import { api, apiErrorMessage } from '$api/client';
	import type { StorageSettings, StorageSaveRequest } from '$types/api';

	let settings = $state<StorageSettings | null>(null);
	let form = $state<StorageSaveRequest>({ enabled: false, bucket: '', region: '', endpoint: '', path_style: false, key_prefix: '', access_key_id: '', secret_access_key: '' });
	let loading = $state(true);
	let busy = $state<'test' | 'save' | null>(null);
	let message = $state<LocalizedText>('');
	let error = $state<LocalizedText>('');

	function applySettings(value: StorageSettings) {
		settings = value;
		form = { enabled: value.enabled, bucket: value.bucket, region: value.region, endpoint: value.endpoint, path_style: value.path_style, key_prefix: value.key_prefix, access_key_id: '', secret_access_key: '' };
	}

	async function loadSettings() {
		loading = true;
		error = '';
		try {
			const result = await api.get<StorageSettings>('/storage');
			if (!result.data) throw new Error('Missing storage settings');
			applySettings(result.data);
		} catch (err) {
			error = apiErrorMessage(err, 'Failed to load storage settings. Please try again.');
		} finally {
			loading = false;
		}
	}

	async function perform(action: 'test' | 'save') {
		if (busy) return;
		busy = action;
		message = '';
		error = '';
		try {
			// Send only form fields; the masked saved key never becomes a credential.
			const payload: StorageSaveRequest = { ...form };
			if (action === 'test') {
				const result = await api.post('/storage/test', payload);
				message = result.message ?? 'Connection verified: upload, read and delete succeeded.';
			} else {
				const result = await api.put<StorageSettings>('/storage', payload);
				if (!result.data) throw new Error('Missing storage settings');
				applySettings(result.data);
				message = 'Storage settings saved.';
			}
		} catch (err) {
			error = apiErrorMessage(err, action === 'test' ? 'Failed to test the S3 connection.' : 'Failed to save storage settings.');
		} finally {
			busy = null;
		}
	}

	onMount(loadSettings);
</script>

<h1>{$t("Storage Settings")}</h1>
<p class="subtitle">{$t("Choose where new documents and versions are stored. Existing files stay in their current storage.")}</p>

{#if error}<div class="notice error" role="alert">{$t(error)}</div>{/if}
{#if message}<div class="notice success" role="status">{$t(message)}</div>{/if}

{#if loading}
	<p class="hint">{$t("Loading storage settings...")}</p>
{:else if !settings}
	<button class="btn-secondary" onclick={loadSettings}>{$t("Retry")}</button>
{:else}
	<form onsubmit={(event) => { event.preventDefault(); perform('save'); }}>
		<fieldset disabled={busy !== null}>
			<legend class="sr-only">{$t("S3 connection settings")}</legend>
			<label class="toggle" for="s3-enabled">
				<input id="s3-enabled" type="checkbox" bind:checked={form.enabled} />
				<span>{$t("Use S3 for new uploads")}</span>
			</label>
			<p class="hint current-storage">{settings.enabled ? $t("New uploads currently use S3.") : $t("New uploads currently use SQLite.")} {$t("Disabling S3 keeps existing S3 files readable and retains the saved keys.")}</p>

			{#if settings.location_locked}
				<p class="notice">{$t("Bucket, endpoint, region and addressing are locked while S3 files or cleanup jobs exist. You can update the keys, change the prefix or disable new S3 uploads.")}</p>
			{/if}

			<div class="row">
				<div class="field">
					<label for="s3-bucket">{$t("Bucket")}</label>
					<input id="s3-bucket" type="text" bind:value={form.bucket} disabled={settings.location_locked} placeholder="company-documents" maxlength="63" spellcheck="false" />
				</div>
				<div class="field">
					<label for="s3-region">{$t("Region")}</label>
					<input id="s3-region" type="text" bind:value={form.region} disabled={settings.location_locked} placeholder="eu-central-1" maxlength="63" spellcheck="false" />
				</div>
			</div>

			<div class="row">
				<div class="field">
					<label for="s3-access-key">{$t("Access Key ID")}</label>
					<input id="s3-access-key" type="text" bind:value={form.access_key_id} autocomplete="off" maxlength="256" spellcheck="false" aria-describedby="key-hint" />
				</div>
				<div class="field">
					<label for="s3-secret-key">{$t("Secret Access Key")}</label>
					<input id="s3-secret-key" type="password" bind:value={form.secret_access_key} autocomplete="new-password" maxlength="512" aria-describedby="key-hint" />
				</div>
			</div>
			<p id="key-hint" class="hint credentials-hint">
				{#if settings.credentials_configured}{$t("Saved key:")} {settings.masked_access_key_id || $t("configured")}{$t(". Leave both key fields empty to keep it, or enter both to replace it.")}{:else}{$t("Enter both keys to connect S3. The secret is stored encrypted and is never displayed again.")}{/if}
			</p>

			<div class="field">
				<label for="s3-endpoint">{$t("Endpoint URL")}</label>
				<input id="s3-endpoint" type="url" bind:value={form.endpoint} disabled={settings.location_locked} placeholder="https://s3.example.com" spellcheck="false" aria-describedby="endpoint-hint" />
				<p id="endpoint-hint" class="hint">{$t("Optional. Leave empty for AWS S3; enter your provider's HTTPS endpoint for an S3-compatible service.")}</p>
			</div>
			<label class="toggle" for="s3-path-style">
				<input id="s3-path-style" type="checkbox" bind:checked={form.path_style} disabled={settings.location_locked} />
				<span>{$t("Use path-style addressing")}</span>
			</label>
			<p class="hint">{$t("Enable only if your S3-compatible provider requires it.")}</p>

			<div class="field prefix-field">
				<label for="s3-prefix">{$t("Key prefix")}</label>
				<input id="s3-prefix" type="text" bind:value={form.key_prefix} placeholder="documents/" maxlength="512" spellcheck="false" />
				<p class="hint">{$t("Optional folder prefix for new objects.")}</p>
			</div>
			<div class="actions">
				<button type="button" class="btn-secondary" onclick={() => perform('test')}>{busy === 'test' ? $t("Testing...") : $t("Test Connection")}</button>
				<button type="submit" class="btn-primary">{busy === 'save' ? $t("Saving...") : $t("Save Settings")}</button>
			</div>
			<p class="hint">{$t("Testing uses the values above without saving. Saving an active connection verifies upload, read and delete access.")}</p>
		</fieldset>
	</form>
{/if}

<section class="setup-guide" aria-labelledby="setup-title">
	<h2 id="setup-title">{$t("Connect S3 in four steps")}</h2>
	<ol>
		<li>{$t("Create a private S3 bucket and keep public access blocked.")}</li>
		<li>{$t("Create an access key with")} <code>{$t("GetObject, PutObject and DeleteObject")}</code> {$t("permissions for the selected bucket/prefix.")}</li>
		<li>{$t("Enter the bucket, region and keys. For an S3-compatible provider, enter its HTTPS endpoint and enable path-style addressing if required.")}</li>
		<li>{$t("Click Test Connection, enable S3 for new uploads, and save. Existing documents remain in their current storage.")}</li>
	</ol>
	<p class="hint">{$t("Files are served through the portal with the existing access rules and PDF watermarks. No public bucket or browser CORS configuration is required.")}</p>
</section>

<style>
	h1 { font-weight: 400; font-size: 1.5rem; margin-bottom: 0.25rem; }
	.subtitle { color: var(--dd-text-secondary); font-size: 0.875rem; margin-bottom: 1.5rem; }
	fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
	.row { display: flex; gap: 1.5rem; }
	.row .field { flex: 1; min-width: 0; }
	.field { margin-bottom: 1rem; }
	.field label { display: block; font-size: 0.75rem; color: var(--dd-text-secondary); margin-bottom: 0.35rem; }
	.field input { box-sizing: border-box; width: 100%; padding: 0.625rem 0.75rem; background: var(--dd-background); border: 1px solid var(--dd-border); color: var(--dd-text); }
	.field input:disabled { opacity: 0.65; cursor: not-allowed; }
	.toggle { display: flex; align-items: center; gap: 0.75rem; font-size: 0.875rem; }
	.hint { color: var(--dd-text-secondary); font-size: 0.75rem; line-height: 1.5; margin-top: 0.5rem; }
	.current-storage { margin-bottom: 1.5rem; }
	.credentials-hint { margin-top: -0.5rem; margin-bottom: 1.5rem; }
	.prefix-field { margin-top: 1.25rem; }
	.notice { padding: 0.875rem; margin-bottom: 1rem; background: var(--dd-surface); border: 1px solid var(--dd-border); font-size: 0.875rem; line-height: 1.5; }
	.error { border-color: var(--dd-danger, #da1e28); }
	.success { border-color: var(--dd-primary); }
	.actions { display: flex; gap: 1rem; margin: 1.5rem 0 0.75rem; }
	.btn-primary, .btn-secondary { padding: 0.625rem 1.25rem; cursor: pointer; }
	.btn-primary { background: var(--dd-primary); color: #fff; border: 1px solid var(--dd-primary); }
	.btn-secondary { background: var(--dd-surface); color: var(--dd-text); border: 1px solid var(--dd-border); }
	fieldset:disabled button { opacity: 0.5; cursor: default; }
	.setup-guide { margin-top: 2rem; padding: 1.25rem; background: var(--dd-surface); border: 1px solid var(--dd-border); }
	h2 { font-size: 1rem; font-weight: 500; margin: 0 0 0.75rem; }
	ol { padding-left: 1.25rem; margin: 0; font-size: 0.875rem; line-height: 1.6; }
	li + li { margin-top: 0.5rem; }
	.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; }
	@media (max-width: 700px) { .row { flex-direction: column; gap: 0; } .actions { flex-wrap: wrap; } }
</style>
