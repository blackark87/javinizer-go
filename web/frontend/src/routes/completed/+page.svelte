<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import {
		CheckCircle2,
		ChevronLeft,
		ChevronRight,
		Clipboard,
		FolderCheck,
		ImageOff,
		LoaderCircle,
		Search,
		SquarePen,
		X,
	} from 'lucide-svelte';
	import { apiClient } from '$lib/api/client';
	import type { CompletedContentItem } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { toastStore } from '$lib/stores/toast';
	import { posterImageCandidates } from '$lib/utils/image';
	import {
		completedActressName,
		completedContentMetadataURL,
		completedContentPageCount,
		completedContentSearchURL,
		completedContentTitle,
	} from './completed-utils';

	const pageSize = 20;

	let contents = $state<CompletedContentItem[]>([]);
	let total = $state(0);
	let query = $state('');
	let activeQuery = $state('');
	let currentPage = $state(1);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let requestSequence = 0;
	let loadedKey = '';
	let posterIndexes = $state<Map<string, number>>(new Map());

	const totalPages = $derived(completedContentPageCount(total, pageSize));
	const rangeStart = $derived(total === 0 ? 0 : (currentPage - 1) * pageSize + 1);
	const rangeEnd = $derived(Math.min(currentPage * pageSize, total));

	$effect(() => {
		const urlQuery = $page.url.searchParams.get('q')?.trim() ?? '';
		const rawPage = Number.parseInt($page.url.searchParams.get('page') ?? '1', 10);
		const urlPage = Number.isFinite(rawPage) && rawPage > 0 ? rawPage : 1;
		const key = `${urlQuery}\u0000${urlPage}`;

		query = urlQuery;
		activeQuery = urlQuery;
		currentPage = urlPage;
		if (key !== loadedKey) {
			loadedKey = key;
			void loadContents(urlQuery, urlPage);
		}
	});

	async function loadContents(searchQuery: string, pageNumber: number) {
		const requestID = ++requestSequence;
		loading = true;
		error = null;

		try {
			const response = await apiClient.listCompletedContent({
				q: searchQuery || undefined,
				limit: pageSize,
				offset: (pageNumber - 1) * pageSize,
			});
			if (requestID !== requestSequence) return;

			contents = response.contents ?? [];
			total = response.total;
			posterIndexes = new Map();

			const resolvedPages = completedContentPageCount(response.total, pageSize);
			if (pageNumber > resolvedPages) {
				await goto(completedContentSearchURL(searchQuery, resolvedPages), {
					replaceState: true,
					noScroll: true,
				});
			}
		} catch (cause) {
			if (requestID !== requestSequence) return;
			contents = [];
			total = 0;
			error = cause instanceof Error ? cause.message : 'Failed to load completed content';
		} finally {
			if (requestID === requestSequence) loading = false;
		}
	}

	function submitSearch(event: SubmitEvent) {
		event.preventDefault();
		void goto(completedContentSearchURL(query, 1), {
			noScroll: true,
			keepFocus: true,
		});
	}

	function clearSearch() {
		query = '';
		void goto('/completed', { noScroll: true, keepFocus: true });
	}

	function changePage(nextPage: number) {
		if (nextPage < 1 || nextPage > totalPages || nextPage === currentPage) return;
		void goto(completedContentSearchURL(activeQuery, nextPage), {
			noScroll: false,
		});
	}

	function advancePoster(content: CompletedContentItem) {
		const next = new Map(posterIndexes);
		next.set(content.movie_id, (next.get(content.movie_id) ?? 0) + 1);
		posterIndexes = next;
	}

	function formatOrganizedAt(value: string): string {
		const date = new Date(value);
		if (Number.isNaN(date.getTime())) return value;
		return new Intl.DateTimeFormat('en-US', {
			dateStyle: 'medium',
			timeStyle: 'short',
		}).format(date);
	}

	async function copyPath(path: string) {
		try {
			await navigator.clipboard.writeText(path);
			toastStore.success('File path copied');
		} catch {
			toastStore.error('Failed to copy the file path');
		}
	}
</script>

<svelte:head>
	<title>Completed Content - Javinizer</title>
</svelte:head>

<div class="container mx-auto max-w-7xl px-4 py-8">
	<div class="mb-6 flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
		<div>
			<div class="flex items-center gap-3">
				<div class="rounded-lg bg-primary/10 p-2 text-primary">
					<FolderCheck class="h-6 w-6" />
				</div>
				<div>
					<h1 class="text-2xl font-bold tracking-tight">Completed Content</h1>
					<p class="text-sm text-muted-foreground">
						Find files that were successfully organized and locate work that needs another pass.
					</p>
				</div>
			</div>
		</div>

		<form class="flex w-full max-w-xl gap-2" onsubmit={submitSearch}>
			<div class="relative flex-1">
				<Search
					class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
				/>
				<input
					bind:value={query}
					type="search"
					placeholder="Search by title or actress"
					aria-label="Search completed content by title or actress"
					class="h-10 w-full rounded-md border border-input bg-background pl-9 pr-9 text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring"
				/>
				{#if query}
					<button
						type="button"
						onclick={clearSearch}
						aria-label="Clear search"
						class="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
					>
						<X class="h-4 w-4" />
					</button>
				{/if}
			</div>
			<Button type="submit" disabled={loading}>
				<Search class="h-4 w-4" />
				Search
			</Button>
		</form>
	</div>

	<div class="mb-4 flex min-h-6 items-center justify-between text-sm text-muted-foreground">
		<p>
			{#if activeQuery}
				{total.toLocaleString()} result{total === 1 ? '' : 's'} for “{activeQuery}”
			{:else}
				{total.toLocaleString()} organized title{total === 1 ? '' : 's'}
			{/if}
		</p>
		{#if total > 0}
			<p>{rangeStart.toLocaleString()}–{rangeEnd.toLocaleString()} of {total.toLocaleString()}</p>
		{/if}
	</div>

	{#if loading}
		<div class="flex min-h-72 items-center justify-center" aria-live="polite">
			<div class="flex items-center gap-3 text-muted-foreground">
				<LoaderCircle class="h-5 w-5 animate-spin" />
				Loading completed content...
			</div>
		</div>
	{:else if error}
		<Card class="border-destructive/40 p-8 text-center">
			<p class="font-medium text-destructive">Could not load completed content</p>
			<p class="mt-1 text-sm text-muted-foreground">{error}</p>
			<div class="mt-4">
				<Button variant="outline" onclick={() => loadContents(activeQuery, currentPage)}>
					Try again
				</Button>
			</div>
		</Card>
	{:else if contents.length === 0}
		<Card class="p-12 text-center">
			<CheckCircle2 class="mx-auto h-10 w-10 text-muted-foreground/60" />
			<h2 class="mt-4 font-semibold">No completed content found</h2>
			<p class="mt-1 text-sm text-muted-foreground">
				{activeQuery
					? 'Try a different title or actress name.'
					: 'Successfully organized files will appear here.'}
			</p>
			{#if activeQuery}
				<div class="mt-4">
					<Button variant="outline" onclick={clearSearch}>Clear search</Button>
				</div>
			{/if}
		</Card>
	{:else}
		<div class="grid gap-4">
			{#each contents as content (content.movie_id)}
				{@const posterCandidates = posterImageCandidates(content)}
				{@const poster = posterCandidates[posterIndexes.get(content.movie_id) ?? 0]}
				<Card class="overflow-hidden">
					<div class="flex flex-col gap-4 p-4 sm:flex-row">
						<div class="h-40 w-full shrink-0 overflow-hidden rounded-md border bg-muted sm:h-40 sm:w-28">
							{#if poster}
								<img
									src={poster}
									alt={`${completedContentTitle(content)} poster`}
									class="h-full w-full object-cover"
									onerror={() => advancePoster(content)}
								/>
							{:else}
								<div class="flex h-full items-center justify-center text-muted-foreground">
									<ImageOff class="h-6 w-6" />
								</div>
							{/if}
						</div>

						<div class="min-w-0 flex-1">
							<div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
								<div class="min-w-0">
									<div class="flex flex-wrap items-center gap-2">
										<span class="rounded bg-primary/10 px-2 py-0.5 text-xs font-semibold text-primary">
											{content.movie_id}
										</span>
										<span class="text-xs text-muted-foreground">
											{formatOrganizedAt(content.organized_at)}
										</span>
									</div>
									<h2 class="mt-2 text-lg font-semibold">{completedContentTitle(content)}</h2>
									{#if content.original_title && content.original_title !== completedContentTitle(content)}
										<p class="mt-0.5 line-clamp-2 text-sm text-muted-foreground">
											{content.original_title}
										</p>
									{/if}
								</div>

								{#if content.content_id}
									<a
										href={completedContentMetadataURL(content.movie_id)}
										class="inline-flex shrink-0 items-center gap-1 text-sm font-medium text-primary hover:underline"
									>
										<SquarePen class="h-4 w-4" />
										Edit Metadata
									</a>
								{/if}
							</div>

							{#if content.actresses.length > 0}
								<div class="mt-3 flex flex-wrap gap-2">
									{#each content.actresses as actress (`${content.movie_id}-${actress.id ?? actress.japanese_name}`)}
										{#if actress.id}
											<a
												href={`/actresses/${actress.id}`}
												class="rounded-full border bg-background px-2.5 py-1 text-xs hover:bg-accent"
											>
												{completedActressName(actress)}
											</a>
										{:else}
											<span class="rounded-full border bg-background px-2.5 py-1 text-xs">
												{completedActressName(actress)}
											</span>
										{/if}
									{/each}
								</div>
							{/if}

							<div class="mt-4 space-y-2">
								{#each content.paths as path (path)}
									<div class="flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2">
										<code class="min-w-0 flex-1 break-all text-xs">{path}</code>
										<button
											type="button"
											onclick={() => copyPath(path)}
											title="Copy file path"
											aria-label={`Copy ${path}`}
											class="shrink-0 rounded p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground"
										>
											<Clipboard class="h-4 w-4" />
										</button>
									</div>
								{/each}
							</div>
						</div>
					</div>
				</Card>
			{/each}
		</div>

		{#if totalPages > 1}
			<nav class="mt-6 flex items-center justify-center gap-3" aria-label="Completed content pages">
				<Button
					variant="outline"
					size="sm"
					disabled={currentPage <= 1}
					onclick={() => changePage(currentPage - 1)}
				>
					<ChevronLeft class="h-4 w-4" />
					Previous
				</Button>
				<span class="text-sm text-muted-foreground">
					Page {currentPage.toLocaleString()} of {totalPages.toLocaleString()}
				</span>
				<Button
					variant="outline"
					size="sm"
					disabled={currentPage >= totalPages}
					onclick={() => changePage(currentPage + 1)}
				>
					Next
					<ChevronRight class="h-4 w-4" />
				</Button>
			</nav>
		{/if}
	{/if}
</div>
