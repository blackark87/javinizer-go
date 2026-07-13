<script lang="ts">
	import { page } from '$app/stores';
	import { createQuery } from '@tanstack/svelte-query';
	import { ArrowLeft, CalendarDays, ImageOff, Star, User } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import type { Actress, Movie } from '$lib/api/types';

	const movieLimit = 24;
	let movieOffset = $state(0);
	let actressImageFailed = $state(false);
	let movieImageFailures = $state(new Set<string>());

	let actressId = $derived(Number($page.params.id));

	const actressQuery = createQuery(() => ({
		queryKey: ['actress', actressId],
		queryFn: () => apiClient.getActress(actressId),
		enabled: Number.isFinite(actressId) && actressId > 0
	}));

	const moviesQuery = createQuery(() => ({
		queryKey: ['actress-movies', actressId, movieLimit, movieOffset],
		queryFn: () => apiClient.listActressMovies(actressId, movieLimit, movieOffset),
		enabled: Number.isFinite(actressId) && actressId > 0
	}));

	function displayName(actress?: Actress): string {
		if (!actress) return 'Actress';
		if (actress.last_name && actress.first_name) return `${actress.last_name} ${actress.first_name}`;
		if (actress.first_name) return actress.first_name;
		if (actress.japanese_name) return actress.japanese_name;
		return `Actress #${actress.id ?? actressId}`;
	}

	function movieTitle(movie: Movie): string {
		return movie.display_title || movie.title || movie.original_title || movie.id || movie.content_id || 'Untitled';
	}

	function movieImage(movie: Movie): string | undefined {
		return movie.poster_url || movie.cropped_poster_url || movie.cover_url;
	}

	function movieKey(movie: Movie): string {
		return movie.content_id || movie.id || movieTitle(movie);
	}

	function releaseLabel(movie: Movie): string {
		if (movie.release_date) return movie.release_date.slice(0, 10);
		if (movie.release_year) return String(movie.release_year);
		return 'Unknown release';
	}
</script>

<div class="max-w-7xl mx-auto px-4 py-6 space-y-6">
	<a href="/actresses" class="inline-flex items-center text-sm text-muted-foreground hover:text-foreground">
		<ArrowLeft class="h-4 w-4 mr-1" /> Back to actresses
	</a>

	{#if actressQuery.isPending}
		<Card class="p-8 text-center text-muted-foreground">Loading actress...</Card>
	{:else if actressQuery.error}
		<Card class="p-8 text-center text-destructive">Failed to load actress: {actressQuery.error.message}</Card>
	{:else if actressQuery.data}
		{@const actress = actressQuery.data}
		<Card class="p-5">
			<div class="flex flex-col md:flex-row gap-5">
				{#if actress.thumb_url && !actressImageFailed}
					<img
						src={apiClient.getPreviewImageURL(actress.thumb_url)}
						alt={displayName(actress)}
						class="w-36 h-44 rounded object-cover border"
						onerror={() => (actressImageFailed = true)}
					/>
				{:else}
					<div class="w-36 h-44 rounded border bg-muted flex items-center justify-center text-muted-foreground">
						<User class="h-10 w-10" />
					</div>
				{/if}

				<div class="flex-1 min-w-0 space-y-4">
					<div>
						<h1 class="text-3xl font-bold tracking-tight">{displayName(actress)}</h1>
						{#if actress.japanese_name}
							<p class="text-lg text-muted-foreground mt-1">{actress.japanese_name}</p>
						{/if}
					</div>

					<div class="flex flex-wrap gap-2">
						{#if actress.id}<span class="text-sm rounded bg-muted px-3 py-1">ID #{actress.id}</span>{/if}
						{#if actress.dmm_id && actress.dmm_id > 0}<span class="text-sm rounded bg-muted px-3 py-1">DMM {actress.dmm_id}</span>{/if}
					</div>

					<div>
						<h2 class="text-sm font-medium mb-1">Aliases</h2>
						<p class="text-sm text-muted-foreground">{actress.aliases || 'No aliases recorded.'}</p>
					</div>
				</div>
			</div>
		</Card>

		<section class="space-y-3">
			<div class="flex items-center justify-between gap-3">
				<div>
					<h2 class="text-2xl font-semibold">Related Movies</h2>
					<p class="text-sm text-muted-foreground">
						Showing {moviesQuery.data?.count ?? 0} of {moviesQuery.data?.total ?? 0} linked movies
					</p>
				</div>
			</div>

			{#if moviesQuery.isPending}
				<Card class="p-8 text-center text-muted-foreground">Loading movies...</Card>
			{:else if moviesQuery.error}
				<Card class="p-8 text-center text-destructive">Failed to load movies: {moviesQuery.error.message}</Card>
			{:else if (moviesQuery.data?.movies ?? []).length === 0}
				<Card class="p-8 text-center text-muted-foreground">No linked movies found.</Card>
			{:else}
				<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
					{#each moviesQuery.data?.movies ?? [] as movie (movieKey(movie))}
						<Card class="overflow-hidden h-full">
							<a href={`/movies/${movie.id || movie.content_id}`} class="block hover:bg-muted/40 h-full">
								{#if movieImage(movie) && !movieImageFailures.has(movieKey(movie))}
									<img
										src={apiClient.getPreviewImageURL(movieImage(movie) ?? '')}
										alt={movieTitle(movie)}
										class="w-full h-64 object-cover border-b"
										onerror={() => {
											movieImageFailures = new Set([...movieImageFailures, movieKey(movie)]);
										}}
									/>
								{:else}
									<div class="w-full h-64 bg-muted flex items-center justify-center text-muted-foreground border-b">
										<ImageOff class="h-8 w-8" />
									</div>
								{/if}
								<div class="p-3 space-y-2">
									<h3 class="font-semibold line-clamp-2">{movieTitle(movie)}</h3>
									<p class="text-xs text-muted-foreground">{movie.id || movie.content_id}</p>
									<div class="flex flex-wrap gap-2 text-xs text-muted-foreground">
										<span class="inline-flex items-center gap-1"><CalendarDays class="h-3 w-3" /> {releaseLabel(movie)}</span>
										{#if movie.rating_score}<span class="inline-flex items-center gap-1"><Star class="h-3 w-3" /> {movie.rating_score}</span>{/if}
									</div>
								</div>
							</a>
						</Card>
					{/each}
				</div>
				<div class="flex items-center justify-end gap-2">
					<Button variant="outline" disabled={movieOffset === 0 || moviesQuery.isFetching} onclick={() => (movieOffset = Math.max(0, movieOffset - movieLimit))}>Previous</Button>
					<Button variant="outline" disabled={movieOffset + movieLimit >= (moviesQuery.data?.total ?? 0) || moviesQuery.isFetching} onclick={() => (movieOffset += movieLimit)}>Next</Button>
				</div>
			{/if}
		</section>
	{/if}
</div>
