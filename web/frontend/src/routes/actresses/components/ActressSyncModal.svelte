<script lang="ts">
	import { fade, scale } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import { CircleAlert, CircleCheck, Loader2, OctagonX, PauseCircle, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import type { ActressSyncSummary } from '../sync-runner';

	let {
		summary,
		isRunning,
		stopRequested,
		currentLabel,
		onStop,
		onClose
	}: {
		summary: ActressSyncSummary;
		isRunning: boolean;
		stopRequested: boolean;
		currentLabel: string;
		onStop: () => void;
		onClose: () => void;
	} = $props();

	let progress = $derived(summary.total > 0 ? Math.round((summary.processed / summary.total) * 100) : 100);

	function fieldLabel(field: string): string {
		if (field === 'dmm_id') return 'DMM ID';
		if (field === 'thumb_url') return 'Profile thumbnail';
		return field;
	}
</script>

<div
	class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
	use:portalToBody
	in:fade|local={{ duration: 150 }}
	out:fade|local={{ duration: 120 }}
	role="presentation"
>
	<div
		class="w-full max-w-2xl"
		in:scale|local={{ start: 0.97, duration: 180, easing: cubicOut }}
		out:scale|local={{ start: 1, opacity: 0.8, duration: 120, easing: cubicOut }}
		role="dialog"
		aria-modal="true"
		aria-labelledby="actress-sync-title"
	>
		<Card class="max-h-[85vh] overflow-hidden flex flex-col">
			<div class="flex items-center justify-between border-b p-5">
				<div>
					<h2 id="actress-sync-title" class="text-xl font-semibold">Actress Metadata Sync</h2>
					<p class="mt-1 text-sm text-muted-foreground">
						{#if isRunning}
							{stopRequested ? 'Stopping after the current actress…' : 'Processing one actress at a time'}
						{:else if summary.stopped}
							Stopped with {summary.total - summary.processed} remaining
						{:else}
							Sync complete
						{/if}
					</p>
				</div>
				{#if !isRunning}
					<Button variant="ghost" size="icon" onclick={onClose} aria-label="Close sync results">
						<X class="h-4 w-4" />
					</Button>
				{/if}
			</div>

			<div class="flex-1 space-y-5 overflow-y-auto p-5">
				<div class="space-y-2">
					<div class="flex items-center justify-between text-sm">
						<span class="font-medium">{isRunning ? currentLabel : 'Processed actresses'}</span>
						<span class="tabular-nums text-muted-foreground">{summary.processed} / {summary.total}</span>
					</div>
					<div class="h-2.5 overflow-hidden rounded-full bg-secondary">
						<div class="h-full rounded-full bg-primary transition-all duration-300" style="width: {progress}%"></div>
					</div>
					<div class="text-right text-xs text-muted-foreground">{progress}%</div>
				</div>

				<div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
					<div class="rounded-lg border bg-muted/20 p-3">
						<div class="text-xs text-muted-foreground">Updated</div>
						<div class="mt-1 text-xl font-semibold text-emerald-600">{summary.updated}</div>
					</div>
					<div class="rounded-lg border bg-muted/20 p-3">
						<div class="text-xs text-muted-foreground">Skipped</div>
						<div class="mt-1 text-xl font-semibold">{summary.skipped}</div>
					</div>
					<div class="rounded-lg border bg-muted/20 p-3">
						<div class="text-xs text-muted-foreground">Conflicts</div>
						<div class="mt-1 text-xl font-semibold text-amber-600">{summary.conflicts}</div>
					</div>
					<div class="rounded-lg border bg-muted/20 p-3">
						<div class="text-xs text-muted-foreground">Failed</div>
						<div class="mt-1 text-xl font-semibold text-destructive">{summary.failed}</div>
					</div>
				</div>

				{#if summary.details.length > 0}
					<div class="space-y-2">
						<h3 class="text-sm font-medium">Per-actress results</h3>
						<div class="max-h-80 space-y-2 overflow-y-auto rounded-lg border p-2">
							{#each summary.details as detail (detail.id)}
								<div class="flex items-start gap-2 rounded-md border bg-background px-3 py-2.5 text-sm">
									{#if detail.status === 'updated'}
										<CircleCheck class="mt-0.5 h-4 w-4 shrink-0 text-emerald-600" />
									{:else if detail.status === 'conflict'}
										<CircleAlert class="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
									{:else if detail.status === 'failed'}
										<OctagonX class="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
									{:else}
										<PauseCircle class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
									{/if}
									<div class="min-w-0 flex-1 space-y-1.5">
										<div class="flex flex-wrap items-center gap-2">
											<span class="font-medium">{detail.label}</span>
											<span class="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">ID {detail.id}</span>
											<span
												class:text-emerald-600={detail.status === 'updated'}
												class:text-amber-600={detail.status === 'conflict'}
												class:text-destructive={detail.status === 'failed'}
												class="ml-auto text-xs font-semibold uppercase"
											>{detail.status}</span>
										</div>

										{#if detail.updatedFields.length > 0}
											<div class="text-xs text-emerald-700">
												Updated: {detail.updatedFields.map(fieldLabel).join(', ')}
											</div>
										{/if}
										{#if detail.source}
											<div class="text-xs text-muted-foreground">
												Source: {detail.source}{detail.sourceQuery ? ` · Query: ${detail.sourceQuery}` : ''}
											</div>
										{/if}
										{#if detail.conflictActressId}
											<div class="text-xs text-amber-700">Conflicts with actress ID {detail.conflictActressId}</div>
										{/if}

										<ul class="space-y-1 text-xs text-muted-foreground">
											{#each detail.messages as message, index (`${detail.id}-${index}`)}
												<li class="flex gap-1.5 break-words">
													<span aria-hidden="true">•</span>
													<span>{message}</span>
												</li>
											{/each}
										</ul>
									</div>
								</div>
							{/each}
						</div>
					</div>
				{/if}
			</div>

			<div class="flex justify-end gap-2 border-t p-4">
				{#if isRunning}
					<Button variant="outline" onclick={onStop} disabled={stopRequested}>
						{#if stopRequested}<Loader2 class="h-4 w-4 animate-spin" />{/if}
						{stopRequested ? 'Stopping…' : 'Stop After Current'}
					</Button>
				{:else}
					<Button onclick={onClose}>Close</Button>
				{/if}
			</div>
		</Card>
	</div>
</div>
