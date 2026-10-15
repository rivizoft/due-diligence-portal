<script lang="ts">
 import { locale, setLocale, t, type Locale } from '$lib/i18n';
 const languages: { code: Locale; label: string; flag: string }[] = [
  {code:'ru', label:'Русский', flag:'🇷🇺'},
  {code:'en', label:'English', flag:'🇬🇧'}
 ];
</script>

<footer class="language-footer">
 <nav class="languages" aria-label={$t('Language')}>
  {#each languages as language}
   <button type="button" lang={language.code} aria-pressed={$locale === language.code}
    class:active={$locale === language.code} onclick={() => setLocale(language.code)}>
    <span aria-hidden="true">{language.flag}</span> {language.label}
   </button>
  {/each}
 </nav>
</footer>

<style>
 .language-footer { display:flex; justify-content:flex-end; padding:0.75rem 1rem; background:var(--dd-background); border-top:1px solid var(--dd-border); }
 .languages { display:flex; flex-wrap:wrap; gap:0.375rem; }
 button { display:inline-flex; align-items:center; gap:0.375rem; min-height:2.75rem; padding:0.5rem 0.75rem; border:1px solid transparent; background:transparent; color:var(--dd-text-secondary); font:inherit; font-size:0.8125rem; cursor:pointer; border-radius:0.25rem; }
 button:hover { background:var(--dd-hover); color:var(--dd-text); }
 button.active { color:var(--dd-text); border-color:var(--dd-primary); background:var(--dd-surface); }
 button:focus-visible { outline:2px solid var(--dd-primary); outline-offset:2px; }
 @media (max-width:480px) { .language-footer { justify-content:center; } }
</style>
