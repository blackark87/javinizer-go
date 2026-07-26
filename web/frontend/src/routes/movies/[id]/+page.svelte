<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import equal from 'fast-deep-equal';
	import { ArrowLeft, LoaderCircle, RotateCcw, Save } from 'lucide-svelte';
	import { apiClient } from '$lib/api/client';
	import type { Movie } from '$lib/api/types';
	import ActressEditor from '$lib/components/ActressEditor.svelte';
	import MovieEditor from '$lib/components/MovieEditor.svelte';
	import ScreenshotManager from '$lib/components/ScreenshotManager.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { toastStore } from '$lib/stores/toast';

	let movie = $state<Movie | null>(null);
	let originalMovie = $state<Movie | null>(null);
	let loading = $state(true);
	let saving = $state(false);
	let retranslatingField = $state<'title' | 'description' | null>(null);
	let error = $state<string | null>(null);
	let loadedID = '';
	let requestSequence = 0;

	const movieID = $derived($page.params.id ?? '');
	const hasChanges = $derived(
		movie !== null && originalMovie !== null && !equal(movie, originalMovie),
	);

	$effect(() => {
		const id = movieID;
		if (!id || id === loadedID) return;
		loadedID = id;
		void loadMovie(id);
	});

	function cloneMovie(value: Movie): Movie {
		return JSON.parse(JSON.stringify(value)) as Movie;
	}

	async function loadMovie(id: string) {
		const requestID = ++requestSequence;
		loading = true;
		error = null;
		try {
			const loaded = await apiClient.getMovie(id);
			if (requestID !== requestSequence) return;
			originalMovie = cloneMovie(loaded);
			movie = cloneMovie(loaded);
		} catch (cause) {
			if (requestID !== requestSequence) return;
			movie = null;
			originalMovie = null;
			error = cause instanceof Error ? cause.message : 'Failed to load movie metadata';
		} finally {
			if (requestID === requestSequence) loading = false;
		}
	}

	function updateMovie(next: Movie) {
		movie = cloneMovie(next);
	}

	function resetChanges() {
		if (!originalMovie) return;
		movie = cloneMovie(originalMovie);
	}

	async function saveMetadata() {
		if (!movie || saving) return;
		saving = true;
		try {
			const payload = cloneMovie(movie);
			if (payload.display_title) {
				payload.title = payload.display_title;
			}
			const saved = await apiClient.updateMovie(movieID, payload);
			originalMovie = cloneMovie(saved);
			movie = cloneMovie(saved);
			toastStore.success('Metadata saved');
		} catch (cause) {
			toastStore.error(
				cause instanceof Error ? cause.message : 'Failed to save movie metadata',
			);
		} finally {
			saving = false;
		}
	}

	async function retranslateField(field: 'title' | 'description') {
		if (!movie || hasChanges || saving || retranslatingField !== null) return;
		retranslatingField = field;
		try {
			const response = await apiClient.reviewMovieTranslation(movieID, { field });
			originalMovie = cloneMovie(response.movie);
			movie = cloneMovie(response.movie);
			if (response.changed) {
				toastStore.success(`${field === 'title' ? 'Title' : 'Description'} retranslated`);
			} else {
				toastStore.success('The reviewed translation was unchanged');
			}
		} catch (cause) {
			toastStore.error(
				cause instanceof Error ? cause.message : 'Failed to retranslate metadata',
			);
		} finally {
			retranslatingField = null;
		}
	}

	function goBack() {
		if (history.length > 1) {
			history.back();
			return;
		}
		void goto('/completed');
	}
</script>

<svelte:head>
	<title>Edit Metadata - Javinizer</title>
</svelte:head>

<div class="container mx-auto max-w-7xl px-4 py-8">
	<div class="mb-6 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
		<div>
			<button
				type="button"
				onclick={goBack}
				class="mb-3 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
			>
				<ArrowLeft class="h-4 w-4" />
				Back
			</button>
			<h1 class="text-3xl font-bold tracking-tight">Edit Metadata</h1>
			{#if movie}
				<p class="mt-1 text-sm text-muted-foreground">
					{movie.id || movie.code} · {movie.display_title || movie.title || movie.original_title}
				</p>
			{:else}
				<p class="mt-1 text-sm text-muted-foreground">{movieID}</p>
			{/if}
		</div>

		{#if movie && originalMovie}
			<div class="flex gap-2">
				<Button
					variant="outline"
					onclick={resetChanges}
					disabled={!hasChanges || saving}
				>
					<RotateCcw class="h-4 w-4" />
					Reset
				</Button>
				<Button onclick={saveMetadata} disabled={!hasChanges || saving}>
					{#if saving}
						<LoaderCircle class="h-4 w-4 animate-spin" />
						Saving...
					{:else}
						<Save class="h-4 w-4" />
						Save Metadata
					{/if}
				</Button>
			</div>
		{/if}
	</div>

	{#if loading}
		<Card class="flex min-h-72 items-center justify-center p-8 text-muted-foreground">
			<LoaderCircle class="mr-2 h-5 w-5 animate-spin" />
			Loading metadata...
		</Card>
	{:else if error}
		<Card class="p-8 text-center">
			<p class="font-medium text-destructive">Could not load movie metadata</p>
			<p class="mt-1 text-sm text-muted-foreground">{error}</p>
			<div class="mt-4">
				<Button variant="outline" onclick={() => loadMovie(movieID)}>Try again</Button>
			</div>
		</Card>
	{:else if movie && originalMovie}
		<div class="space-y-6">
			<Card class="p-6">
				<h2 class="mb-4 text-xl font-semibold">Movie Metadata</h2>
				<MovieEditor
					{movie}
					{originalMovie}
					onUpdate={updateMovie}
					onRetranslate={retranslateField}
					{retranslatingField}
					retranslationDisabled={hasChanges || saving}
					identifiersReadonly={true}
				/>
			</Card>

			<Card class="p-6">
				<h2 class="mb-4 text-xl font-semibold">Cast</h2>
				<ActressEditor
					{movie}
					onUpdate={updateMovie}
					savingEdits={saving}
					organizing={false}
				/>
			</Card>

			<Card class="p-6">
				<h2 class="mb-4 text-xl font-semibold">Images &amp; Media</h2>
				<ScreenshotManager {movie} onUpdate={updateMovie} />
			</Card>

			<div class="sticky bottom-4 flex justify-end rounded-lg border bg-background/95 p-3 shadow-lg backdrop-blur">
				<Button onclick={saveMetadata} disabled={!hasChanges || saving}>
					{#if saving}
						<LoaderCircle class="h-4 w-4 animate-spin" />
						Saving...
					{:else}
						<Save class="h-4 w-4" />
						Save Metadata
					{/if}
				</Button>
			</div>
		</div>
	{/if}
</div>
