<script lang="ts">
	import { api } from '$api/client';

	interface WatermarkConfig {
		enabled: boolean;
		text_template: string;
		position: string;
		opacity: number;
		font_size: number;
		color: string;
	}

	let config = $state<WatermarkConfig | null>(null);
	let loading = $state(true);
	let saving = $state(false);
	let message = $state('');
 let previewText = $derived(config?.text_template.replace(/\{\{(user_email|user_name|date|document_name)\}\}/g, (_, key: string) => ({ user_email: 'investor@example.com', user_name: 'Alex Investor', date: new Date().toISOString().slice(0, 10), document_name: 'Report.pdf' })[key as 'user_email' | 'user_name' | 'date' | 'document_name']) ?? '');

	async function loadConfig() {
		try {
			const res = await api.get<WatermarkConfig>('/watermark');
			config = res.data ?? null;
		} catch {
			config = null;
		}
		loading = false;
	}

	async function saveConfig() {
		if (!config) return;
		saving = true;
		message = '';
		try {
			await api.put('/watermark', config);
			message = 'Watermark settings saved.';
		} catch (e) {
			message = 'Failed to save watermark settings.';
		}
		saving = false;
	}

	async function resetConfig() {
		try {
			await api.delete('/watermark');
			await loadConfig();
			message = 'Watermark settings reset to defaults.';
		} catch {
			message = 'Failed to reset.';
		}
	}

	$effect(() => { loadConfig(); });
</script>

<h1>Watermark Settings</h1>
<p class="subtitle">Configure personal watermarks applied to PDF viewing and downloads. Originals are preserved.</p>

{#if loading}
	<p class="loading">Loading...</p>
{:else if !config}
	<p>Failed to load watermark configuration.</p>
{:else}
	{#if message}
		<div class="message">{message}</div>
	{/if}

	<section>
		<div class="field toggle-field">
			<label for="enabled">Enable Watermarks</label>
			<input id="enabled" type="checkbox" bind:checked={config.enabled} />
			<span class="toggle-label">{config.enabled ? 'Enabled' : 'Disabled'}</span>
		</div>

		<div class="field">
			<label for="template">Text Template</label>
			<input id="template" type="text" bind:value={config.text_template}
				placeholder={'{{user_email}} - {{date}}'} />
			<span class="hint">Variables: {'{{user_email}}'}, {'{{user_name}}'}, {'{{date}}'}, {'{{document_name}}'}</span>
		</div>

		<div class="field">
			<label for="position">Position</label>
			<select id="position" bind:value={config.position}>
				<option value="diagonal">Diagonal</option>
				<option value="top">Top</option>
				<option value="bottom">Bottom</option>
				<option value="center">Center</option>
			</select>
		</div>

		<div class="row">
			<div class="field">
				<label for="opacity">Opacity</label>
				<input id="opacity" type="range" min="0" max="1" step="0.05"
					bind:value={config.opacity} />
				<span class="value">{config.opacity}</span>
			</div>

			<div class="field">
				<label for="fontSize">Font Size</label>
				<input id="fontSize" type="number" min="6" max="72"
					bind:value={config.font_size} />
			</div>

			<div class="field">
				<label for="color">Color</label>
				<div class="color-input">
					<input type="color" bind:value={config.color} />
					<input type="text" bind:value={config.color} />
				</div>
			</div>
		</div>
	</section>

	<div class="preview" aria-label="Illustrative watermark preview">
  <div class="sample-content"><strong>Sample document</strong><p>Confidential information</p><hr /><hr /><hr /><hr /></div>
  {#if config.enabled}<div class="sample-watermark {config.position}" style:opacity={config.opacity} style:color={config.color} style:font-size={`${config.font_size * 0.6}px`}>{previewText}</div>{/if}
 </div>
 <p class="hint">Illustrative preview. PDF font and layout may differ. Dates use UTC (YYYY-MM-DD).</p>
 <p class="hint">View-only users receive page images with the watermark. Screenshots remain possible. Applying a watermark can invalidate a PDF digital signature.</p>

	<div class="actions">
		<button class="btn-primary" onclick={saveConfig} disabled={saving}>
			{saving ? 'Saving...' : 'Save Settings'}
		</button>
		<button class="btn-secondary" onclick={resetConfig}>Reset to Defaults</button>
	</div>
{/if}

<style>
	h1 { font-weight: 400; font-size: 1.5rem; margin-bottom: 0.25rem; }
	.subtitle { color: var(--dd-text-secondary); font-size: 0.875rem; margin-bottom: 2rem; }

	section { margin-bottom: 2rem; }

	.field { margin-bottom: 1.25rem; }
	.field label { display: block; font-size: 0.75rem; color: var(--dd-text-secondary); margin-bottom: 0.25rem; }
	.field input[type="text"], .field input[type="number"], .field select {
		width: 100%; padding: 0.5rem 0.75rem;
		background: var(--dd-background); border: 1px solid var(--dd-border); color: var(--dd-text);
	}
	.hint { font-size: 0.6875rem; color: var(--dd-text-secondary); }

	.toggle-field { display: flex; align-items: center; gap: 0.75rem; }
	.toggle-field label { margin-bottom: 0; }
	.toggle-label { font-size: 0.8125rem; color: var(--dd-text-secondary); }

	.row { display: flex; gap: 1.5rem; }
	.row .field { flex: 1; }
	.value { font-size: 0.75rem; color: var(--dd-text-secondary); margin-left: 0.5rem; }

	.color-input { display: flex; gap: 0.5rem; align-items: center; }
	.color-input input[type="color"] { width: 36px; height: 36px; border: none; cursor: pointer; background: none; }
	.color-input input[type="text"] {
		flex: 1; padding: 0.375rem 0.5rem;
		background: var(--dd-background); border: 1px solid var(--dd-border);
		color: var(--dd-text); font-family: monospace; font-size: 0.8125rem;
	}


 .preview {position:relative;aspect-ratio:595/842;max-width:320px;margin:1.5rem auto;background:#fff;border:1px solid var(--dd-border);overflow:hidden;color:#555;}
 .sample-content {padding:2rem;font-size:.75rem;}
 .sample-content hr{margin:1.5rem 0;border:0;border-top:1px solid #ddd;}
 .sample-watermark {position:absolute;left:5%;right:5%;text-align:center;overflow-wrap:anywhere;line-height:1.3;}
 .sample-watermark.center,.sample-watermark.diagonal{top:50%;transform:translateY(-50%);}
 .sample-watermark.diagonal{transform:translateY(-50%) rotate(-45deg);}
 .sample-watermark.top{top:3%;}
 .sample-watermark.bottom{bottom:3%;}

	.actions { display: flex; gap: 1rem; }
	.btn-primary { padding: 0.625rem 1.25rem; background: var(--dd-primary); color: #fff; border: none; cursor: pointer; }
	.btn-primary:disabled { opacity: 0.5; cursor: default; }
	.btn-secondary { padding: 0.625rem 1.25rem; background: var(--dd-surface); color: var(--dd-text); border: 1px solid var(--dd-border); cursor: pointer; }
	.message { padding: 0.75rem; background: var(--dd-surface); border: 1px solid var(--dd-primary); margin-bottom: 1rem; }
	.loading { color: var(--dd-text-secondary); }
</style>
