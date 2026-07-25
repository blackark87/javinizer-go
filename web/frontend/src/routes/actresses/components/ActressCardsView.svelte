<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { Pencil, Trash2, ImageOff } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import type { Actress } from '$lib/api/types';
	import { shouldToggleCardFromClick, shouldToggleCardFromKey } from './card-selection';

	let {
		actresses,
		selectedIds,
		itemDelay,
		getDisplayName,
		isSelected,
		onToggleSelection,
		onStartEdit,
		onRemoveActress,
		deletePending
	}: {
		actresses: Actress[];
		selectedIds: number[];
		itemDelay: (index: number) => number;
		getDisplayName: (actress: Actress) => string;
		isSelected: (actress: Actress) => boolean;
		onToggleSelection: (actress: Actress) => void;
		onStartEdit: (actress: Actress) => void;
		onRemoveActress: (actress: Actress) => void;
		deletePending: boolean;
	} = $props();

	// Reactive per-image error state — reset when the list changes so a
	// refreshed (same-URL) image re-fetches instead of staying hidden by a
	// stale onerror display:none from a prior failed load.
	let actressImgErrors = $state<Set<string | undefined>>(new Set());
	$effect(() => { actresses; actressImgErrors = new Set(); });

	function handleCardClick(event: MouseEvent, actress: Actress) {
		if (!actress.id || !shouldToggleCardFromClick(event.target)) return;
		onToggleSelection(actress);
	}

	function handleCardKeydown(event: KeyboardEvent, actress: Actress) {
		if (!actress.id || !shouldToggleCardFromKey(event)) return;
		event.preventDefault();
		onToggleSelection(actress);
	}
</script>

<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
	{#each actresses as actress, index (`${actress.id ?? 'na'}-${index}`)}
		<div animate:flip={{ duration: 220, easing: quintOut }} in:fly|local={{ y: 10, duration: 220, delay: itemDelay(index), easing: quintOut }}>
			<Card class="h-full {isSelected(actress) ? 'ring-2 ring-primary' : ''}">
				<div
					class="flex h-full cursor-pointer items-start gap-3 rounded-lg p-3 outline-none transition-colors hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
					role="checkbox"
					aria-checked={isSelected(actress)}
					aria-disabled={!actress.id}
					aria-label={`Select ${getDisplayName(actress)} for merge`}
					tabindex={actress.id ? 0 : -1}
					onclick={(event) => handleCardClick(event, actress)}
					onkeydown={(event) => handleCardKeydown(event, actress)}
				>
					<div class="pt-1">
						<input
							type="checkbox"
							checked={isSelected(actress)}
							disabled={!actress.id}
							onchange={() => onToggleSelection(actress)}
							aria-label="Select actress for merge"
							class="rounded border-input"
						/>
					</div>
					{#if actress.thumb_url && !actressImgErrors.has(actress.thumb_url)}
						{#if actress.id}
							<a href={`/actresses/${actress.id}`} aria-label={`${getDisplayName(actress)} details`}>
								<img
									src={apiClient.getPreviewImageURL(actress.thumb_url)}
									alt={getDisplayName(actress)}
									class="w-20 h-24 rounded object-cover border transition-opacity hover:opacity-90"
									onerror={() => { actressImgErrors = new Set([...actressImgErrors, actress.thumb_url]); }}
								/>
							</a>
						{:else}
							<img
								src={apiClient.getPreviewImageURL(actress.thumb_url)}
								alt={getDisplayName(actress)}
								class="w-20 h-24 rounded object-cover border"
								onerror={() => { actressImgErrors = new Set([...actressImgErrors, actress.thumb_url]); }}
							/>
						{/if}
					{:else}
						<div class="w-20 h-24 rounded border bg-muted flex items-center justify-center text-muted-foreground">
							<ImageOff class="h-4 w-4" />
						</div>
					{/if}

					<div class="flex-1 min-w-0">
						<div class="flex flex-wrap items-center gap-2">
							<h3 class="font-semibold truncate">
								{#if actress.id}
									<a href={`/actresses/${actress.id}`} class="hover:underline">{getDisplayName(actress)}</a>
								{:else}
									{getDisplayName(actress)}
								{/if}
							</h3>
							{#if actress.id}
								<span class="text-xs rounded bg-muted px-2 py-0.5">#{actress.id}</span>
							{/if}
							{#if actress.dmm_id && actress.dmm_id > 0}
								<span class="text-xs rounded bg-muted px-2 py-0.5">DMM {actress.dmm_id}</span>
							{/if}
						</div>
						{#if actress.japanese_name}
							<p class="text-sm text-muted-foreground truncate">{actress.japanese_name}</p>
						{/if}
						{#if actress.aliases}
							<p class="text-xs text-muted-foreground line-clamp-2 mt-1">Aliases: {actress.aliases}</p>
						{/if}
						<div class="flex items-center gap-2 mt-3">
							<Button variant="outline" size="sm" onclick={() => onStartEdit(actress)}>
								<Pencil class="h-4 w-4" />
								Edit
							</Button>
							<Button
								variant="outline"
								size="sm"
								onclick={() => onRemoveActress(actress)}
								disabled={deletePending}
								class="text-destructive hover:bg-destructive/10"
							>
								<Trash2 class="h-4 w-4" />
								Delete
							</Button>
						</div>
					</div>
				</div>
			</Card>
		</div>
	{/each}
</div>
