<script lang="ts">
	import type { ScrapeCandidate } from '$lib/api/types';

	type CandidatePreview = ScrapeCandidate & {
		release_date?: string;
		actresses?: Array<string | { name?: string; japanese_name?: string; first_name?: string; last_name?: string }>;
		genres?: Array<string | { name?: string }>;
		maker?: string;
		label?: string;
		series?: string;
		description?: string;
		cover_url?: string;
	};

	let {
		candidate,
		disabled = false,
		onSelect
	}: {
		candidate: CandidatePreview;
		disabled?: boolean;
		onSelect: (source: string) => void;
	} = $props();

	const title = $derived(candidate.title || '(no title)');
	const previewImageUrl = $derived(candidate.poster_url || candidate.cover_url);
	const makerLabelSeries = $derived(
		[candidate.maker, candidate.label, candidate.series].filter(Boolean).join(' / ')
	);

	function formatPeople(items: CandidatePreview['actresses']) {
		return items
			?.map((item) => {
				if (typeof item === 'string') return item;
				return item.name || item.japanese_name || [item.first_name, item.last_name].filter(Boolean).join(' ');
			})
			.filter(Boolean)
			.join(', ');
	}

	function formatGenres(items: CandidatePreview['genres']) {
		return items
			?.map((item) => (typeof item === 'string' ? item : item.name))
			.filter(Boolean)
			.join(', ');
	}

	const actresses = $derived(formatPeople(candidate.actresses));
	const genres = $derived(formatGenres(candidate.genres));
</script>

<div class="candidate-preview group relative">
	<button
		type="button"
		class="w-full text-left rounded-md border p-3 hover:bg-muted focus:bg-muted focus:outline-none focus:ring-2 focus:ring-ring transition-colors disabled:opacity-50"
		disabled={disabled}
		onclick={() => onSelect(candidate.source)}
		aria-describedby={`candidate-preview-${candidate.source}`}
	>
		<div class="text-xs font-medium uppercase text-muted-foreground">{candidate.source}</div>
		<div class="text-sm font-medium whitespace-normal break-words" title={title}>{title}</div>
		{#if candidate.original_title && candidate.original_title !== candidate.title}
			<div class="text-xs text-muted-foreground whitespace-normal break-words" title={candidate.original_title}>
				{candidate.original_title}
			</div>
		{/if}
		<div class="text-xs text-muted-foreground">
			{candidate.actress_count} actress{candidate.actress_count === 1 ? '' : 'es'}
		</div>
	</button>

	<div
		id={`candidate-preview-${candidate.source}`}
		class="pointer-events-none fixed inset-x-3 bottom-3 z-50 max-h-[70vh] overflow-auto rounded-lg border bg-popover p-4 text-popover-foreground opacity-0 shadow-xl transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100 md:absolute md:inset-x-auto md:bottom-auto md:left-[calc(100%+0.75rem)] md:top-0 md:w-96"
		role="tooltip"
	>
		<div class="space-y-3">
			<div class="flex gap-3">
				{#if previewImageUrl}
					<img
						src={previewImageUrl}
						alt={`Poster preview for ${title}`}
						class="h-32 w-24 shrink-0 rounded border object-cover"
					/>
				{:else}
					<div class="flex h-32 w-24 shrink-0 items-center justify-center rounded border bg-muted text-xs text-muted-foreground">
						No image
					</div>
				{/if}
				<div class="min-w-0 space-y-1">
					<div class="text-xs font-medium uppercase text-muted-foreground">{candidate.source}</div>
					<h4 class="text-sm font-semibold leading-snug whitespace-normal break-words">{title}</h4>
					{#if candidate.original_title && candidate.original_title !== candidate.title}
						<p class="text-xs text-muted-foreground whitespace-normal break-words">{candidate.original_title}</p>
					{/if}
				</div>
			</div>

			<div class="grid grid-cols-[6.5rem_1fr] gap-x-3 gap-y-1 text-xs">
				<div class="text-muted-foreground">Movie ID</div><div>{candidate.movie_id || '—'}</div>
				<div class="text-muted-foreground">Release date</div><div>{candidate.release_date || '—'}</div>
				<div class="text-muted-foreground">Actresses</div><div class="whitespace-normal break-words">{actresses || `${candidate.actress_count} listed`}</div>
				<div class="text-muted-foreground">Genres</div><div class="whitespace-normal break-words">{genres || '—'}</div>
				<div class="text-muted-foreground">Maker / Label / Series</div><div class="whitespace-normal break-words">{makerLabelSeries || '—'}</div>
			</div>

			{#if candidate.description}
				<p class="text-xs leading-relaxed text-muted-foreground whitespace-pre-wrap break-words">
					{candidate.description}
				</p>
			{/if}
		</div>
	</div>
</div>
