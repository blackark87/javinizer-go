<script lang="ts">
	import { fade, scale } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import { CircleAlert, CircleCheck, Clock3, Loader2, OctagonX, PauseCircle, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import type { ActressSyncSummary } from '../sync-runner';

	let {
		summary,
		isRunning,
		stopRequested,
		onStop,
		onClose
	}: {
		summary: ActressSyncSummary;
		isRunning: boolean;
		stopRequested: boolean;
		onStop: () => void;
		onClose: () => void;
	} = $props();

	let progress = $derived(summary.total > 0 ? Math.round((summary.processed / summary.total) * 100) : 100);

	function fieldLabel(field: string): string {
		return ({
			dmm_id: 'DMM ID', thumb_url: 'Profile thumbnail', japanese_name: 'Japanese name',
			hepburn_name: 'Hepburn name', movie_actresses: 'Movie mappings',
			movie_translation_actresses: 'Translated movie cast', nfo: 'NFO actors'
		} as Record<string, string>)[field] ?? field;
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
		class="w-full max-w-3xl"
		in:scale|local={{ start: 0.97, duration: 180, easing: cubicOut }}
		out:scale|local={{ start: 1, opacity: 0.8, duration: 120, easing: cubicOut }}
		role="dialog"
		aria-modal="true"
		aria-labelledby="actress-sync-title"
	>
		<Card class="max-h-[88vh] overflow-hidden flex flex-col">
			<div class="flex items-center justify-between border-b p-5">
				<div>
					<h2 id="actress-sync-title" class="text-xl font-semibold">Actress Metadata Sync</h2>
					<p class="mt-1 text-sm text-muted-foreground">
						{#if isRunning}
							{stopRequested ? 'Stopping after all running items finish…' : `${summary.active.length} item(s) currently running in the background`}
						{:else if summary.stopped}
							Stopped · {summary.cancelled} queued item(s) cancelled
						{:else}
							Sync complete
						{/if}
					</p>
				</div>
				<Button variant="ghost" size="icon" onclick={onClose} aria-label="Close sync progress">
					<X class="h-4 w-4" />
				</Button>
			</div>

			<div class="flex-1 space-y-5 overflow-y-auto p-5">
				<div class="space-y-2">
					<div class="flex items-center justify-between text-sm">
						<span class="font-medium">Background task progress</span>
						<span class="tabular-nums text-muted-foreground">{summary.processed} / {summary.total}</span>
					</div>
					<div class="h-2.5 overflow-hidden rounded-full bg-secondary">
						<div class="h-full rounded-full bg-primary transition-all duration-300" style="width: {progress}%"></div>
					</div>
					<div class="text-right text-xs text-muted-foreground">{progress}%</div>
				</div>

				<div class="grid grid-cols-3 gap-2 sm:grid-cols-6">
					{#each [
						['Updated', summary.updated, 'text-emerald-600'], ['Warnings', summary.warnings, 'text-amber-600'],
						['Skipped', summary.skipped, ''], ['Conflicts', summary.conflicts, 'text-amber-600'],
						['Failed', summary.failed, 'text-destructive'], ['Cancelled', summary.cancelled, 'text-muted-foreground']
					] as stat}
						<div class="rounded-lg border bg-muted/20 p-3">
							<div class="text-xs text-muted-foreground">{stat[0]}</div>
							<div class={`mt-1 text-xl font-semibold ${stat[2]}`}>{stat[1]}</div>
						</div>
					{/each}
				</div>

				{#if summary.active.length > 0}
					<div class="space-y-2">
						<h3 class="text-sm font-medium">Currently running</h3>
						<div class="grid gap-2 sm:grid-cols-2">
							{#each summary.active as detail (detail.id)}
								<div class="flex items-center gap-2 rounded-md border bg-primary/5 px-3 py-2 text-sm">
									<Loader2 class="h-4 w-4 shrink-0 animate-spin text-primary" />
									<div class="min-w-0">
										<div class="truncate font-medium">{detail.label}</div>
										<div class="text-xs capitalize text-muted-foreground">{detail.stage}</div>
									</div>
								</div>
							{/each}
						</div>
					</div>
				{/if}

				{#if summary.details.length > 0}
					<div class="space-y-2">
						<h3 class="text-sm font-medium">Item details</h3>
						<div class="max-h-96 space-y-2 overflow-y-auto rounded-lg border p-2">
							{#each summary.details as detail (detail.id)}
								<div class="flex items-start gap-2 rounded-md border bg-background px-3 py-2.5 text-sm">
									{#if detail.status === 'completed'}
										<CircleCheck class="mt-0.5 h-4 w-4 shrink-0 text-emerald-600" />
									{:else if detail.status === 'conflict'}
										<CircleAlert class="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
									{:else if detail.status === 'failed'}
										<OctagonX class="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
									{:else if detail.status === 'running'}
										<Loader2 class="mt-0.5 h-4 w-4 shrink-0 animate-spin text-primary" />
									{:else if detail.status === 'pending'}
										<Clock3 class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
									{:else}
										<PauseCircle class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
									{/if}
									<div class="min-w-0 flex-1 space-y-1.5">
										<div class="flex flex-wrap items-center gap-2">
											<span class="font-medium">{detail.label}</span>
											<span class="rounded bg-muted px-1.5 py-0.5 text-[11px] capitalize text-muted-foreground">{detail.stage}</span>
											<span class="ml-auto text-xs font-semibold uppercase">{detail.outcome || detail.status}</span>
										</div>
										{#if detail.updatedFields.length > 0}
											<div class="text-xs text-emerald-700">Updated: {detail.updatedFields.map(fieldLabel).join(', ')}</div>
										{/if}
										{#if detail.warning}<div class="text-xs text-amber-700">Warning: {detail.warning}</div>{/if}
										{#if detail.errorMessage}<div class="text-xs text-destructive">{detail.errorMessage}</div>{/if}
										{#if detail.messages.length > 0}
											<ul class="space-y-1 text-xs text-muted-foreground">
												{#each detail.messages as message, index (`${detail.id}-${index}`)}
													<li class="flex gap-1.5 break-words"><span aria-hidden="true">•</span><span>{message}</span></li>
												{/each}
											</ul>
										{/if}
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
						{stopRequested ? 'Stopping…' : 'Stop After Running Items'}
					</Button>
				{/if}
				<Button onclick={onClose}>{isRunning ? 'Run in Background' : 'Close'}</Button>
			</div>
		</Card>
	</div>
</div>
