<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { cubicOut, quintOut } from 'svelte/easing';
	import { fade, fly, scale } from 'svelte/transition';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { Plus, RefreshCw, Download, Upload, Loader2 } from 'lucide-svelte';
	import { apiClient } from '$lib/api/client';
	import type { Actress, ActressSyncJob, ActressSyncTask, ActressUpsertRequest, ImportResponse } from '$lib/api/types';
	import { toastStore } from '$lib/stores/toast';
	import { confirmDialog } from '$lib/stores/dialog.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { createActressStore } from './stores/actress-store.svelte';
	import ActressForm from './components/ActressForm.svelte';
	import ActressToolbar from './components/ActressToolbar.svelte';
	import ActressCardsView from './components/ActressCardsView.svelte';
	import ActressCompactView from './components/ActressCompactView.svelte';
	import ActressTableView from './components/ActressTableView.svelte';
	import ActressMergeModal from './components/ActressMergeModal.svelte';
	import ActressPagination from './components/ActressPagination.svelte';
	import ActressSyncModal from './components/ActressSyncModal.svelte';
	import { buildActressSyncSummary } from './sync-runner';

	const store = createActressStore();
	const queryClient = useQueryClient();
	let importFile = $state<HTMLInputElement | null>(null);
	let syncModalOpen = $state(false);
	let syncPreparing = $state(false);
	let syncJob = $state<ActressSyncJob | null>(null);
	let syncTasks = $state<ActressSyncTask[]>([]);
	let syncPollTimer: ReturnType<typeof setInterval> | null = null;
	let notifiedJobID: string | null = null;
	let syncRunning = $derived(syncJob?.status === 'pending' || syncJob?.status === 'running');
	let syncStopRequested = $derived(syncJob?.cancel_requested ?? false);
	let syncSummary = $derived(syncJob ? buildActressSyncSummary(syncJob, syncTasks) : null);
	let isSyncing = $derived(syncRunning || syncPreparing);

	const exportMutation = createMutation(() => ({
		mutationFn: () => apiClient.exportActresses(),
		onSuccess: async (data: Actress[]) => {
			const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = 'actresses.json';
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
			toastStore.success(`Exported ${data.length} actress(es)`, 3000);
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to export actresses', 4000);
		}
	}));

	const importMutation = createMutation(() => ({
		mutationFn: (payload: { actresses: ActressUpsertRequest[] }) =>
			apiClient.importActresses(payload),
		onSuccess: (res: ImportResponse) => {
			toastStore.success(`Import complete — Imported: ${res.imported}, Skipped: ${res.skipped}, Errors: ${res.errors}`, 5000);
			void queryClient.invalidateQueries({ queryKey: ['actresses'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to import actresses', 4000);
		}
	}));

	function handleExport() {
		exportMutation.mutate();
	}

	function handleImportClick() {
		importFile?.click();
	}

	async function handleImportChange(e: Event) {
		const target = e.target as HTMLInputElement;
		const file = target.files?.[0];
		if (!file) return;

		try {
			const text = await file.text();
			const parsed: ActressUpsertRequest[] = JSON.parse(text);
			if (!Array.isArray(parsed)) throw new Error('Expected a JSON array');

			const actresses = parsed.filter(a => a.first_name || a.japanese_name);

			if (actresses.length === 0) {
				toastStore.error('No valid actresses in file', 4000);
				return;
			}

			if (!confirm(`Import ${actresses.length} actress(es)?`)) return;

			importMutation.mutate({ actresses });
		} catch (err) {
			toastStore.error(`Invalid JSON file: ${err instanceof Error ? err.message : String(err)}`, 4000);
		}

		target.value = '';
	}

	function stopPolling() {
		if (syncPollTimer) clearInterval(syncPollTimer);
		syncPollTimer = null;
	}

	async function refreshSyncJob(jobID: string) {
		const [jobResponse, tasksResponse] = await Promise.all([
			apiClient.getActressSyncJob(jobID),
			apiClient.listActressSyncJobTasks(jobID)
		]);
		if (syncJob?.id && syncJob.id !== jobID) return;
		syncJob = jobResponse.job;
		syncTasks = tasksResponse.tasks;
		if (jobResponse.job.status === 'completed' || jobResponse.job.status === 'cancelled') {
			stopPolling();
			await queryClient.invalidateQueries({ queryKey: ['actresses'] });
			if (notifiedJobID !== jobID) {
				notifiedJobID = jobID;
				const job = jobResponse.job;
				toastStore.success(`Sync ${job.status} — Updated: ${job.updated}, Warnings: ${job.warnings}, Skipped: ${job.skipped}, Conflicts: ${job.conflicts}, Failed: ${job.failed}`, 6000);
			}
		}
	}

	function startPolling(jobID: string) {
		stopPolling();
		syncPollTimer = setInterval(() => {
			void refreshSyncJob(jobID).catch((error) => toastStore.error(error instanceof Error ? error.message : 'Failed to refresh actress sync job', 4000));
		}, 1000);
	}

	async function attachSyncJob(job: ActressSyncJob) {
		syncJob = job;
		syncTasks = [];
		syncModalOpen = true;
		await refreshSyncJob(job.id);
		if (job.status === 'pending' || job.status === 'running') startPolling(job.id);
	}

	async function startSyncJob(request: { scope: 'missing' | 'selected'; missing?: boolean; actress_ids?: number[] }) {
		const response = await apiClient.createActressSyncJob(request);
		notifiedJobID = null;
		await attachSyncJob(response.job);
	}

	async function handleSyncMissing() {
		if (isSyncing) return;
		syncPreparing = true;
		try {
			const candidates = await apiClient.listActressSyncCandidates();
			if (candidates.total === 0) {
				toastStore.success('All actresses already have a DMM ID and profile thumbnail');
				return;
			}
			const confirmed = await confirmDialog(
				'Sync Missing Actress Metadata',
				`Queue ${candidates.total} actress(es) for background sync? Unknown actresses expand into one task per linked movie.`,
				{ confirmLabel: 'Start Sync' }
			);
			if (confirmed) await startSyncJob({ scope: 'missing', missing: true });
		} catch (error) {
			toastStore.error(error instanceof Error ? error.message : 'Failed to load sync candidates', 4000);
		} finally {
			syncPreparing = false;
		}
	}

	async function handleSyncSelected() {
		if (isSyncing || store.selectedIds.length === 0) return;
		syncPreparing = true;
		try {
			const ids = [...store.selectedIds];
			const confirmed = await confirmDialog(
				'Sync Selected Actress Metadata',
				`Queue ${ids.length} selected actress(es) for background sync?`,
				{ confirmLabel: 'Start Sync' }
			);
			if (confirmed) await startSyncJob({ scope: 'selected', actress_ids: ids });
		} catch (error) {
			toastStore.error(error instanceof Error ? error.message : 'Failed to sync selected actresses', 4000);
		} finally {
			syncPreparing = false;
		}
	}

	async function requestSyncStop() {
		if (!syncJob) return;
		try {
			syncJob = (await apiClient.cancelActressSyncJob(syncJob.id)).job;
		} catch (error) {
			toastStore.error(error instanceof Error ? error.message : 'Failed to stop actress sync', 4000);
		}
	}

	function closeSyncModal() {
		syncModalOpen = false;
	}

	onMount(() => {
		void apiClient.listActiveActressSyncJobs()
			.then((response) => response.jobs.length > 0 ? attachSyncJob(response.jobs[0]) : undefined)
			.catch(() => undefined);
	});

	onDestroy(() => {
		// Only browser polling stops; the durable server job continues.
		stopPolling();
	});
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		<div
			class="flex flex-wrap items-center justify-between gap-3"
			in:fly|local={{ y: -10, duration: 240, easing: cubicOut }}
		>
			<div>
				<h1 class="text-3xl font-bold">Actress Database</h1>
				<p class="text-muted-foreground mt-1">Create, update, and remove actress records stored in the database.</p>
			</div>
			<div class="flex items-center gap-2">
				<input
					type="file"
					accept=".json"
					bind:this={importFile}
					onchange={handleImportChange}
					class="hidden"
				/>
				<Button
					variant="outline"
					size="sm"
					onclick={handleSyncMissing}
					disabled={isSyncing}
				>
					{#if syncPreparing}
						<Loader2 class="h-4 w-4 animate-spin" />
					{:else}
						<RefreshCw class="h-4 w-4" />
					{/if}
					Sync Missing
				</Button>
				<Button
					variant="outline"
					size="sm"
					onclick={handleExport}
					disabled={exportMutation.isPending}
				>
					{#if exportMutation.isPending}
						<Loader2 class="h-4 w-4 animate-spin mr-1" />
					{:else}
						<Download class="h-4 w-4 mr-1" />
					{/if}
					Export
				</Button>
				<Button
					variant="outline"
					size="sm"
					onclick={handleImportClick}
					disabled={importMutation.isPending}
				>
					{#if importMutation.isPending}
						<Loader2 class="h-4 w-4 animate-spin mr-1" />
					{:else}
						<Upload class="h-4 w-4 mr-1" />
					{/if}
					Import
				</Button>
				<Button variant="outline" onclick={store.refresh}>
					<RefreshCw class="h-4 w-4 {store.isRefreshing ? 'animate-spin' : ''}" />
					Refresh
				</Button>
				<Button onclick={store.resetForm}>
					<Plus class="h-4 w-4" />
					New Actress
				</Button>
			</div>
		</div>

		<div class="grid grid-cols-1 xl:grid-cols-5 gap-6" in:fade|local={{ duration: 240 }}>
			<div class="xl:col-span-2 xl:self-start xl:sticky xl:top-20">
				<ActressForm
					editingId={store.editingId}
					bind:form={store.form}
					formError={store.formError}
					isPending={store.saveActressMutation.isPending}
					onSave={store.saveActress}
					onReset={store.resetForm}
				/>
			</div>

			<div class="xl:col-span-3 space-y-4">
				<ActressToolbar
					bind:queryInput={store.queryInput}
					activeQuery={store.activeQuery}
					bind:viewMode={store.viewMode}
					bind:sortBy={store.sortBy}
					sortOrder={store.sortOrder}
					selectedIds={store.selectedIds}
					total={store.total}
					actressesCount={store.actresses.length}
					isRefreshing={store.isRefreshing}
					onApplySearch={store.applySearch}
					onClearSearch={store.clearSearch}
					onToggleSortOrder={store.toggleSortOrder}
					onSelectCurrentPage={store.selectCurrentPage}
					onClearSelection={store.clearSelection}
					onSyncSelected={handleSyncSelected}
					onStartMergeSelected={store.startMergeSelected}
					onDeleteSelected={store.removeSelected}
					onDeleteAll={store.removeAll}
					{isSyncing}
				/>

				{#if store.error}
					<div in:fly|local={{ y: 8, duration: 180 }}>
						<Card class="p-4 border-destructive bg-destructive/10 text-destructive">
							{store.error}
						</Card>
					</div>
				{/if}

				{#if store.loading}
					<div in:fade|local={{ duration: 180 }}>
						<Card class="p-8 text-center text-muted-foreground">Loading actresses...</Card>
					</div>
				{:else if store.actresses.length === 0}
					<div in:fade|local={{ duration: 180 }}>
						<Card class="p-8 text-center">
							<p class="text-muted-foreground">No actresses found.</p>
						</Card>
					</div>
				{:else}
					{#key store.viewMode}
						<div in:scale|local={{ start: 0.98, duration: 180, easing: quintOut }} out:fade|local={{ duration: 120 }}>
							{#if store.viewMode === 'cards'}
								<ActressCardsView
									actresses={store.actresses}
									selectedIds={store.selectedIds}
									itemDelay={store.itemDelay}
									getDisplayName={store.getDisplayName}
									isSelected={store.isSelected}
									onToggleSelection={store.toggleSelection}
									onStartEdit={store.startEdit}
									onRemoveActress={store.removeActress}
									deletePending={store.deleteActressMutation.isPending}
								/>
							{:else if store.viewMode === 'compact'}
								<ActressCompactView
									actresses={store.actresses}
									itemDelay={store.itemDelay}
									getDisplayName={store.getDisplayName}
									isSelected={store.isSelected}
									onToggleSelection={store.toggleSelection}
									onStartEdit={store.startEdit}
									onRemoveActress={store.removeActress}
									deletePending={store.deleteActressMutation.isPending}
								/>
							{:else}
								<ActressTableView
									actresses={store.actresses}
									itemDelay={store.itemDelay}
									getDisplayName={store.getDisplayName}
									isSelected={store.isSelected}
									onToggleSelection={store.toggleSelection}
									onStartEdit={store.startEdit}
									onRemoveActress={store.removeActress}
									deletePending={store.deleteActressMutation.isPending}
								/>
							{/if}
						</div>
					{/key}
				{/if}

				<ActressPagination
					currentPage={store.currentPage}
					totalPages={store.totalPages}
					canGoPrev={store.canGoPrev}
					canGoNext={store.canGoNext}
					onPrevPage={store.prevPage}
					onNextPage={store.nextPage}
				/>
			</div>
		</div>
	</div>
</div>

<ActressMergeModal
	bind:showMergeModal={store.showMergeModal}
	selectedIds={store.selectedIds}
	bind:mergePrimaryId={store.mergePrimaryId}
	mergeSourceQueue={store.mergeSourceQueue}
	mergeCurrentSourceId={store.mergeCurrentSourceId}
	bind:mergeResolutions={store.mergeResolutions}
	mergePreview={store.mergePreview}
	mergePreviewFetching={store.mergePreviewQuery.isFetching}
	mergeSummary={store.mergeSummary}
	mergePending={store.mergeActressMutation.isPending}
	getActressLabelByID={store.getActressLabelByID}
	onCloseMergeModal={store.closeMergeModal}
	onResetMergeQueueAndPreview={store.resetMergeQueueAndPreview}
	onApplyCurrentMerge={store.applyCurrentMerge}
	onSkipCurrentMerge={store.skipCurrentMerge}
	onSetResolution={store.setResolution}
	formatMergeValue={store.formatMergeValue}
/>

{#if syncModalOpen && syncSummary}
	<ActressSyncModal
		summary={syncSummary}
		isRunning={syncRunning}
		stopRequested={syncStopRequested}
		onStop={requestSyncStop}
		onClose={closeSyncModal}
	/>
{/if}
